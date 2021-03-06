/*
	Go Language Raspberry Pi Interface
	(c) Copyright David Thorpe 2016-2018
	All Rights Reserved
    Documentation http://djthorpe.github.io/gopi/
	For Licensing and Usage information, please see LICENSE.md
*/

package gopi

import (
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

////////////////////////////////////////////////////////////////////////////////
// TYPES

// AppConfig defines how an application should be created
type AppConfig struct {
	Modules  []*Module
	AppArgs  []string
	AppFlags *Flags
	Debug    bool
	Verbose  bool
	Service  string
}

// AppInstance defines the running application instance with modules
type AppInstance struct {
	AppFlags *Flags
	Logger   Logger
	Hardware Hardware
	Display  Display
	Graphics SurfaceManager
	Input    InputManager
	Fonts    FontManager
	Layout   Layout
	Timer    Timer
	GPIO     GPIO
	I2C      I2C
	SPI      SPI
	LIRC     LIRC
	debug    bool
	verbose  bool
	service  string
	sigchan  chan os.Signal
	modules  []*Module
	byname   map[string]Driver
	bytype   map[ModuleType]Driver
	byorder  []Driver
}

// MainTask defines a function which can run as a main task
// and has a channel which can be written to when the task
// has completed
type MainTask func(app *AppInstance, done chan<- struct{}) error

// BackgroundTask defines a function which can run as a
// background task and has a channel which receives a value of gopi.DONE
// then the background task should complete
type BackgroundTask func(app *AppInstance, done <-chan struct{}) error

////////////////////////////////////////////////////////////////////////////////
// GLOBAL VARIABLES

const (
	// DEFAULT_RPC_SERVICE is the default service type
	DEFAULT_RPC_SERVICE = "gopi"
)

var (
	// DONE is the message sent on the channel to indicate task is completed
	DONE = struct{}{}
)

////////////////////////////////////////////////////////////////////////////////
// PUBLIC METHODS

// NewAppConfig method will create a new configuration file given the set of
// modules which should be created, the arguments are either by type
// or by name
func NewAppConfig(modules ...string) AppConfig {
	var err error

	config := AppConfig{}

	// retrieve modules and dependencies, using appendModule
	if config.Modules, err = ModuleWithDependencies("logger"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return AppConfig{}
	}

	// append other modules
	if config.Modules, err = AppendModulesByName(config.Modules, modules...); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return AppConfig{}
	}

	// Set the flags
	config.AppArgs = getTestlessArguments(os.Args[1:])
	config.AppFlags = NewFlags(path.Base(os.Args[0]))
	config.Debug = false
	config.Verbose = false
	config.Service = DEFAULT_RPC_SERVICE

	// Set 'debug' and 'verbose' flags
	config.AppFlags.FlagBool("debug", false, "Set debugging mode")
	config.AppFlags.FlagBool("verbose", false, "Verbose logging")

	// Call module.Config for each module
	for _, module := range config.Modules {
		if module.Config != nil {
			module.Config(&config)
		}
	}

	// Return the configuration
	return config
}

// NewAppInstance method will create a new application object given an application
// configuration
func NewAppInstance(config AppConfig) (*AppInstance, error) {

	if config.AppFlags == nil {
		return nil, ErrAppError
	}

	// Parse flags. We want to ignore flags which start with "-test."
	// in the testing environment
	if config.AppFlags != nil && config.AppFlags.Parsed() == false {
		if err := config.AppFlags.Parse(config.AppArgs); err != nil {
			return nil, err
		}
	}

	// Set debug and verbose flags
	if debug, exists := config.AppFlags.GetBool("debug"); exists {
		config.Debug = debug
	}
	if verbose, exists := config.AppFlags.GetBool("verbose"); exists {
		config.Verbose = verbose
	}

	// Create instance
	this := new(AppInstance)
	this.debug = config.Debug
	this.verbose = config.Verbose
	this.AppFlags = config.AppFlags

	// Set service name from configuration or the name
	// from AppFlags
	if config.Service != "" {
		this.service = config.Service
	} else {
		this.service = this.AppFlags.Name()
	}

	// Set up signalling
	this.sigchan = make(chan os.Signal, 1)
	signal.Notify(this.sigchan, syscall.SIGTERM, syscall.SIGINT)

	// Set module maps
	this.modules = config.Modules
	this.byname = make(map[string]Driver, len(config.Modules))
	this.bytype = make(map[ModuleType]Driver, len(config.Modules))
	this.byorder = make([]Driver, 0, len(config.Modules))

	// Create module instances
	var once sync.Once
	for _, module := range config.Modules {
		// Report open (once after logger module is created)
		if this.Logger != nil {
			once.Do(func() {
				this.Logger.Debug("gopi.AppInstance.Open(){ modules=%v }", config.Modules)
			})
		}
		if module.New != nil {
			if this.Logger != nil {
				this.Logger.Debug2("module.New{ %v }", module)
			}
			if driver, err := module.New(this); err != nil {
				return nil, err
			} else if driver == nil {
				return nil, fmt.Errorf("%v: New: return nil", module.Name)
			} else if err := this.setModuleInstance(module, driver); err != nil {
				if err := driver.Close(); err != nil {
					this.Logger.Error("module.Close(): %v", err)
				}
				return nil, err
			}
		}
	}

	// report Open() again if it's not been done yet
	once.Do(func() {
		this.Logger.Debug("gopi.AppInstance.Open()")
	})

	// success
	return this, nil
}

// Run all tasks simultaneously, the first task in the list on the main thread and the
// remaining tasks background tasks.
func (this *AppInstance) Run(main_task MainTask, background_tasks ...BackgroundTask) error {
	// Lock this to run in the current operating system thread (ie, the main thread)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Call the Run method for each module. If any report an error, then don't run
	// the application. Note that some modules don't have a 'New' method in which
	// case the driver argument is set to nil
	for _, module := range this.modules {
		if module.Run == nil {
			continue
		}
		driver, _ := this.byname[module.Name]
		if err := module.Run(this, driver); err != nil {
			return err
		}
	}

	// create the channels we'll use to signal the goroutines
	channels := make([]chan struct{}, len(background_tasks)+1)
	channels[0] = make(chan struct{})
	for i := range background_tasks {
		channels[i+1] = make(chan struct{})
	}

	// if more than one task, then give them a channel which is signalled
	// by the main thread for ending
	var wg sync.WaitGroup
	if len(background_tasks) > 0 {
		for i, task := range background_tasks {
			wg.Add(1)
			go func(i int, t BackgroundTask) {
				defer wg.Done()
				if err := t(this, channels[i+1]); err != nil {
					if this.Logger != nil {
						this.Logger.Error("Error: %v [background_task %v]", err, i+1)
					}
				}
			}(i, task)
		}
	}

	go func() {
		// Wait for mainDone
		_ = <-channels[0]
		if this.Logger != nil {
			this.Logger.Debug2("Main thread done")
		}
		// Signal other tasks to complete
		for i := 0; i < len(background_tasks); i++ {
			this.Logger.Debug2("Sending DONE to background task %v of %v", i+1, len(background_tasks))
			channels[i+1] <- DONE
		}
	}()

	// Now run main task
	err := main_task(this, channels[0])

	// Wait for other tasks to finish
	if len(background_tasks) > 0 {
		if this.Logger != nil {
			this.Logger.Debug2("Waiting for tasks to finish")
		}
	}
	wg.Wait()
	if this.Logger != nil {
		this.Logger.Debug2("All tasks finished")
	}

	return err
}

// Debug returns whether the application has the debug flag set
func (this *AppInstance) Debug() bool {
	return this.debug
}

// Verbose returns whether the application has the verbose flag set
func (this *AppInstance) Verbose() bool {
	return this.verbose
}

// Service returns the current service name set from configuration
func (this *AppInstance) Service() string {
	return this.service
}

// WaitForSignal blocks until a signal is caught
func (this *AppInstance) WaitForSignal() {
	s := <-this.sigchan
	this.Logger.Debug2("gopi.AppInstance.WaitForSignal: %v", s)
}

// WaitForSignalOrTimeout blocks until a signal is caught or
// timeout occurs and return true if the signal is caught
func (this *AppInstance) WaitForSignalOrTimeout(timeout time.Duration) bool {
	select {
	case s := <-this.sigchan:
		this.Logger.Debug2("gopi.AppInstance.WaitForSignalOrTimeout: %v", s)
		return true
	case <-time.After(timeout):
		return false
	}
}

// SendSignal will send the terminate signal, breaking the WaitForSignal
// block
func (this *AppInstance) SendSignal() error {
	if process, err := os.FindProcess(os.Getpid()); err != nil {
		return err
	} else if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	return nil
}

// Close method for app
func (this *AppInstance) Close() error {
	this.Logger.Debug("gopi.AppInstance.Close()")

	// In reverse order, call the Close method on each
	// driver
	for i := len(this.byorder); i > 0; i-- {
		driver := this.byorder[i-1]
		this.Logger.Debug2("gopi.AppInstance.Close() %v", driver)
		if err := driver.Close(); err != nil {
			this.Logger.Error("gopi.AppInstance.Close() error: %v", err)
		}
	}

	// Clear out the references
	this.bytype = nil
	this.byname = nil
	this.byorder = nil

	this.Layout = nil
	this.Display = nil
	this.Graphics = nil
	this.Fonts = nil
	this.Hardware = nil
	this.Logger = nil
	this.Timer = nil
	this.Input = nil
	this.I2C = nil
	this.GPIO = nil
	this.SPI = nil
	this.LIRC = nil

	// Return success
	return nil
}

// ModuleInstance returns module instance by name, or returns nil if the module
// cannot be found. You can use reserved words (ie, logger, layout, etc)
// for common module types
func (this *AppInstance) ModuleInstance(name string) Driver {
	var instance Driver
	// Check for reserved words
	if module_type, exists := module_name_map[name]; exists {
		instance, _ = this.bytype[module_type]
	} else {
		instance, _ = this.byname[name]
	}
	return instance
}

// Append Modules by name onto the configuration
func AppendModulesByName(modules []*Module, names ...string) ([]*Module, error) {
	if modules == nil {
		modules = make([]*Module, 0, len(names))
	}
	for _, name := range names {
		if module_array, err := ModuleWithDependencies(name); err != nil {
			return nil, err
		} else {
			modules = appendModules(modules, module_array)
		}
	}
	return modules, nil
}

////////////////////////////////////////////////////////////////////////////////
// PRIVATE METHODS

// appendModules adds modules from 'others' onto 'modules' without
// creating duplicate modules
func appendModules(modules []*Module, others []*Module) []*Module {
	if len(others) == 0 {
		return modules
	}
	for _, other := range others {
		if inModules(modules, other) {
			continue
		}
		modules = append(modules, other)
	}
	return modules
}

// inModules returns true if a module is in the array
func inModules(modules []*Module, other *Module) bool {
	for _, module := range modules {
		if module == other {
			return true
		}
	}
	return false
}

func (this *AppInstance) setModuleInstance(module *Module, driver Driver) error {
	var ok bool

	// Set by name. Currently returns an error if there is more than one module with the same name
	if _, exists := this.byname[module.Name]; exists {
		return fmt.Errorf("setModuleInstance: Duplicate module with name '%v'", module.Name)
	} else {
		this.byname[module.Name] = driver
	}

	// Set by type. Currently returns an error if there is more than one module with the same type
	// Allows multiple modules accessed by name if other, service or client
	if module.Type != MODULE_TYPE_NONE && module.Type != MODULE_TYPE_OTHER && module.Type != MODULE_TYPE_SERVICE && module.Type != MODULE_TYPE_CLIENT {
		if _, exists := this.bytype[module.Type]; exists {
			return fmt.Errorf("setModuleInstance: Duplicate module with type '%v'", module.Type)
		} else {
			this.bytype[module.Type] = driver
		}
	}

	// Append to list of modules in the order (so we can close in the right order
	// later)
	this.byorder = append(this.byorder, driver)

	// Now some convenience methods for already-cast drivers
	switch module.Type {
	case MODULE_TYPE_LOGGER:
		if this.Logger, ok = driver.(Logger); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.Logger", module)
		}
	case MODULE_TYPE_HARDWARE:
		if this.Hardware, ok = driver.(Hardware); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.Hardware", module)
		}
	case MODULE_TYPE_DISPLAY:
		if this.Display, ok = driver.(Display); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.Display", module)
		}
	case MODULE_TYPE_GRAPHICS:
		if this.Graphics, ok = driver.(SurfaceManager); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.SurfaceManager", module)
		}
	case MODULE_TYPE_FONTS:
		if this.Fonts, ok = driver.(FontManager); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.FontManager", module)
		}
	case MODULE_TYPE_LAYOUT:
		if this.Layout, ok = driver.(Layout); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.Layout", module)
		}
	case MODULE_TYPE_GPIO:
		if this.GPIO, ok = driver.(GPIO); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.GPIO", module)
		}
	case MODULE_TYPE_I2C:
		if this.I2C, ok = driver.(I2C); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.I2C", module)
		}
	case MODULE_TYPE_SPI:
		if this.SPI, ok = driver.(SPI); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.SPI", module)
		}
	case MODULE_TYPE_TIMER:
		if this.Timer, ok = driver.(Timer); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.Timer", module)
		}
	case MODULE_TYPE_LIRC:
		if this.LIRC, ok = driver.(LIRC); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.LIRC", module)
		}
	case MODULE_TYPE_INPUT:
		if this.Input, ok = driver.(InputManager); !ok {
			return fmt.Errorf("Module %v cannot be cast to gopi.InputManager", module)
		}
	}
	// success
	return nil
}

func getTestlessArguments(input []string) []string {
	output := make([]string, 0, len(input))
	for _, arg := range input {
		if strings.HasPrefix(arg, "-test.") {
			continue
		}
		output = append(output, arg)
	}
	return output
}

////////////////////////////////////////////////////////////////////////////////
// STRINGIFY

func (this *AppInstance) String() string {
	modules := make([]string, 0, len(this.byname))
	for k := range this.byname {
		modules = append(modules, k)
	}
	return fmt.Sprintf("gopi.App{ debug=%v verbose=%v service=%v modules=%v instances=%v }", this.debug, this.verbose, this.service, modules, this.byorder)
}

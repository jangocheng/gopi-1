/*
	Go Language Raspberry Pi Interface
	(c) Copyright David Thorpe 2016
	All Rights Reserved

	For Licensing and Usage information, please see LICENSE.md
*/

// This example takes a snapshot of the screen and writes to a file as a PNG
// image
package main

import (
	"fmt"
	"os"
)

import (
	app "github.com/djthorpe/gopi/app"
	khronos "github.com/djthorpe/gopi/khronos"
)

////////////////////////////////////////////////////////////////////////////////

func MyRunLoop(app *app.App) error {
	egl := app.EGL.(khronos.EGLDriver)

	// Do snapshot
	bitmap, err := egl.SnapshotImage()
	if err != nil {
		return err
	}
	defer egl.DestroyImage(bitmap)

	// Save file as PNG
	surface, err := egl.CreateSurfaceWithBitmap(bitmap, khronos.EGLPoint{100, 100}, 2, 0.5)
	if err != nil {
		return err
	}
	defer egl.DestroySurface(surface)

	app.WaitUntilDone()

	// Return success
	return nil
}

////////////////////////////////////////////////////////////////////////////////

func main() {
	// Create the config
	config := app.Config(app.APP_EGL)

	// Create the application
	myapp, err := app.NewApp(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
	defer myapp.Close()

	// Run the application
	if err := myapp.Run(MyRunLoop); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
}
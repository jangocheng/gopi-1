package main

import (
	"github.com/djthorpe/gopi/rpi/egl"
	"log"
)

func main() {
	egl.BCMHostInit()

	// Initalize display
	display := egl.GetDisplay()
	if err := egl.Initialize(display, nil, nil); err != nil {
		log.Fatalf("Unable to initalize display: %v", err)
	}

	// Choose configuration
	attr := []int32{
		egl.EGL_RED_SIZE, 8,
		egl.EGL_GREEN_SIZE, 108,
		egl.EGL_BLUE_SIZE, 8,
		egl.EGL_ALPHA_SIZE, 8,
		egl.EGL_SURFACE_TYPE, egl.EGL_WINDOW_BIT,
		egl.EGL_NONE,
	}

	var (
		config    egl.Config
		numConfig int32
	)

	if err := egl.ChooseConfig(display, attr, &config, 1, &numConfig); err != nil {
		log.Fatalf("Unable to initalize display: %v", err)
	}

	/*
		if ok := egl.GetConfigAttrib(display, config, egl.NATIVE_VISUAL_ID, &vid); !ok {
			egl.LogError(egl.GetError())
		}
		egl.BindAPI(egl.OPENGL_ES_API)
		context = egl.CreateContext(display, config, egl.NO_CONTEXT, &ctxAttr[0])

		screen_width, screen_height = egl.GraphicsGetDisplaySize(0)
		log.Printf("Display size W: %d H: %d\n", screen_width, screen_height)
	*/

	// Terminate display
	if err := egl.Terminate(display); err != nil {
		log.Fatalf("Unable to terminate display: %v", err)
	}
}

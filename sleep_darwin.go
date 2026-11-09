package main

/*
#cgo LDFLAGS: -framework Cocoa
void startSleepWatch(void);
*/
import "C"

// sleepWatchApp is the app notified of sleep/wake. Set once by watchSleep before
// the NSApplication run loop starts; the exported callbacks run on the main
// thread while that loop is live.
var sleepWatchApp *App

//export goDidSleep
func goDidSleep() {
	if sleepWatchApp != nil {
		sleepWatchApp.pauseForSleep(false)
	}
}

//export goSystemWillSleep
func goSystemWillSleep() {
	if sleepWatchApp != nil {
		sleepWatchApp.pauseForSleep(true)
	}
}

//export goDidWake
func goDidWake() {
	if sleepWatchApp != nil {
		sleepWatchApp.resumeAfterWake()
	}
}

// watchSleep registers for display and system sleep/wake notifications so timers
// pause while the Mac sleeps and opted-in ones also while just the display does.
// Call it before menuet starts its run loop.
func watchSleep(a *App) {
	sleepWatchApp = a
	C.startSleepWatch()
}

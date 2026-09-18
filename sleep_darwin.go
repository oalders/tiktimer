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
		sleepWatchApp.pauseForSleep()
	}
}

//export goDidWake
func goDidWake() {
	if sleepWatchApp != nil {
		sleepWatchApp.resumeAfterWake()
	}
}

// watchSleep registers for display and system sleep/wake notifications so opted-in
// timers pause while the Mac is asleep. Call it before menuet starts its run loop.
func watchSleep(a *App) {
	sleepWatchApp = a
	C.startSleepWatch()
}

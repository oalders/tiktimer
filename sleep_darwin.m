#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

// SleepWatcher forwards NSWorkspace sleep/wake notifications to the exported Go
// callbacks. Screen sleep ("display turned off / walked away") pauses opted-in
// timers; system sleep pauses every running timer so maintenance wakes add no
// time. Both paths are idempotent on the Go side.
@interface SleepWatcher : NSObject
@end

@implementation SleepWatcher
- (void)screensDidSleep:(NSNotification *)note {
    goScreenDidSleep();
}
- (void)systemWillSleep:(NSNotification *)note {
    goSystemWillSleep();
}
- (void)didWake:(NSNotification *)note {
    goDidWake();
}
@end

void startSleepWatch(void) {
    static SleepWatcher *watcher = nil;
    if (watcher != nil) {
        return;
    }
    watcher = [[SleepWatcher alloc] init];

    NSNotificationCenter *center = [[NSWorkspace sharedWorkspace] notificationCenter];
    [center addObserver:watcher
               selector:@selector(screensDidSleep:)
                   name:NSWorkspaceScreensDidSleepNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(didWake:)
                   name:NSWorkspaceScreensDidWakeNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(systemWillSleep:)
                   name:NSWorkspaceWillSleepNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(didWake:)
                   name:NSWorkspaceDidWakeNotification
                 object:nil];
}

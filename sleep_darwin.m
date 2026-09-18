#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

// SleepWatcher forwards NSWorkspace sleep/wake notifications to the exported Go
// callbacks. Screen sleep covers "display turned off / walked away"; system
// sleep covers a closed lid or full sleep (and keeps the frozen-clock interval
// from being counted). Both paths are idempotent on the Go side.
@interface SleepWatcher : NSObject
@end

@implementation SleepWatcher
- (void)didSleep:(NSNotification *)note {
    goDidSleep();
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
               selector:@selector(didSleep:)
                   name:NSWorkspaceScreensDidSleepNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(didWake:)
                   name:NSWorkspaceScreensDidWakeNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(didSleep:)
                   name:NSWorkspaceWillSleepNotification
                 object:nil];
    [center addObserver:watcher
               selector:@selector(didWake:)
                   name:NSWorkspaceDidWakeNotification
                 object:nil];
}

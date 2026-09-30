#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

@interface AQPrefsController : NSObject
@property(strong) NSWindow *window;
@property(strong) NSButton *notify;
@property(strong) NSButton *notifyReset;
@property(strong) NSPopUpButton *threshold;
@property(strong) NSPopUpButton *pin;
@property(strong) NSPopUpButton *interval;
@end

@implementation AQPrefsController
- (void)changed:(id)sender {
  if (sender == self.notify) {
    aqPrefChanged(0, self.notify.state == NSControlStateValueOn);
  } else if (sender == self.notifyReset) {
    aqPrefChanged(1, self.notifyReset.state == NSControlStateValueOn);
  } else if (sender == self.threshold) {
    aqPrefChanged(2, (int)self.threshold.indexOfSelectedItem);
  } else if (sender == self.pin) {
    aqPrefChanged(3, (int)self.pin.indexOfSelectedItem);
  } else if (sender == self.interval) {
    aqPrefChanged(4, (int)self.interval.indexOfSelectedItem);
  }
}
@end

static AQPrefsController *controller;

static NSTextField *formLabel(NSString *text) {
  NSTextField *label = [NSTextField labelWithString:text];
  label.alignment = NSTextAlignmentRight;
  return label;
}

static NSTextField *hintLabel(NSString *text) {
  NSTextField *label = [NSTextField wrappingLabelWithString:text];
  label.font = [NSFont systemFontOfSize:[NSFont smallSystemFontSize]];
  label.textColor = [NSColor secondaryLabelColor];
  label.preferredMaxLayoutWidth = 240;
  return label;
}

static NSPopUpButton *popup(void) {
  NSPopUpButton *button = [[NSPopUpButton alloc] initWithFrame:NSZeroRect pullsDown:NO];
  button.target = controller;
  button.action = @selector(changed:);
  [button.widthAnchor constraintGreaterThanOrEqualToConstant:200].active = YES;
  return button;
}

static void buildWindow(void) {
  controller = [AQPrefsController new];
  controller.notify = [NSButton checkboxWithTitle:@"Send a notification when a limit reaches the alert level"
                                           target:controller
                                           action:@selector(changed:)];
  controller.notifyReset = [NSButton checkboxWithTitle:@"Send a notification when a limit resets"
                                                target:controller
                                                action:@selector(changed:)];
  controller.threshold = popup();
  controller.pin = popup();
  controller.interval = popup();

  NSView *none = [NSGridCell emptyContentView];
  NSGridView *grid = [NSGridView gridViewWithViews:@[
    @[ formLabel(@"Alerts:"), controller.notify ],
    @[ none, controller.notifyReset ],
    @[ formLabel(@"Alert level:"), controller.threshold ],
    @[ none, hintLabel(@"You get an alert when a limit reaches this usage, and again at 100%.") ],
    @[ formLabel(@"Menu bar shows:"), controller.pin ],
    @[ none, hintLabel(@"The menu always shows all providers.") ],
    @[ formLabel(@"Refresh every:"), controller.interval ],
  ]];
  grid.translatesAutoresizingMaskIntoConstraints = NO;
  grid.rowSpacing = 8;
  grid.columnSpacing = 10;
  grid.rowAlignment = NSGridRowAlignmentFirstBaseline;
  [grid columnAtIndex:0].xPlacement = NSGridCellPlacementTrailing;
  [grid rowAtIndex:2].topPadding = 12;
  [grid rowAtIndex:4].topPadding = 12;
  [grid rowAtIndex:6].topPadding = 12;

  NSWindow *window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 480, 280)
                                                 styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable
                                                   backing:NSBackingStoreBuffered
                                                     defer:NO];
  window.title = @"aiquokka Preferences";
  window.releasedWhenClosed = NO;
  NSView *content = window.contentView;
  [content addSubview:grid];
  [NSLayoutConstraint activateConstraints:@[
    [grid.leadingAnchor constraintEqualToAnchor:content.leadingAnchor constant:28],
    [grid.trailingAnchor constraintEqualToAnchor:content.trailingAnchor constant:-28],
    [grid.topAnchor constraintEqualToAnchor:content.topAnchor constant:24],
    [grid.bottomAnchor constraintEqualToAnchor:content.bottomAnchor constant:-24],
  ]];
  [content layoutSubtreeIfNeeded];
  [window setContentSize:content.fittingSize];
  [window center];
  controller.window = window;
}

static void fill(NSPopUpButton *button, NSArray<NSString *> *items, int selected) {
  [button removeAllItems];
  [button addItemsWithTitles:items];
  if (selected >= 0 && selected < (int)items.count) {
    [button selectItemAtIndex:selected];
  }
}

static NSArray<NSString *> *lines(const char *joined) {
  return [[NSString stringWithUTF8String:joined] componentsSeparatedByString:@"\n"];
}

void aqShowPreferences(bool notify, bool notifyReset, const char *thresholds, int threshold, const char *pins, int pin, const char *intervals, int interval) {
  NSArray<NSString *> *thresholdItems = lines(thresholds);
  NSArray<NSString *> *pinItems = lines(pins);
  NSArray<NSString *> *intervalItems = lines(intervals);
  dispatch_async(dispatch_get_main_queue(), ^{
    if (controller == nil) {
      buildWindow();
    }
    controller.notify.state = notify ? NSControlStateValueOn : NSControlStateValueOff;
    controller.notifyReset.state = notifyReset ? NSControlStateValueOn : NSControlStateValueOff;
    fill(controller.threshold, thresholdItems, threshold);
    fill(controller.pin, pinItems, pin);
    fill(controller.interval, intervalItems, interval);
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    [NSApp activateIgnoringOtherApps:YES];
    [controller.window makeKeyAndOrderFront:nil];
  });
}

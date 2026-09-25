//go:build darwin

package history

// Popup panel with recent dictations. Opened by hotkey; a click (or ↑/↓ and
// Return) copies the full text to the clipboard, Esc or clicking elsewhere
// closes it. The panel is non-activating, so the app the user was typing in
// keeps focus and the copied text can be pasted right away.

/*
#cgo LDFLAGS: -framework Cocoa
#cgo CFLAGS: -x objective-c -fobjc-arc

#import <Cocoa/Cocoa.h>
#include <stdlib.h>

static const CGFloat kPanelW = 380;
static const CGFloat kRowH = 58;
static const CGFloat kHeaderH = 28;
static const CGFloat kPad = 10;

@class WWHistoryPanel;

@interface WWHistoryRow : NSView
@property (nonatomic, copy) NSString *fullText;
@property (nonatomic, copy) NSString *timeText;
@property (nonatomic, assign) BOOL hovered;
@property (nonatomic, assign) BOOL copied;
@property (nonatomic, strong) NSTextField *timeLabel;
@property (nonatomic, strong) NSTextField *textLabel;
@property (nonatomic, weak) WWHistoryPanel *panel;
@end

@interface WWHistoryPanel : NSPanel <NSWindowDelegate>
@property (nonatomic, strong) NSMutableArray<WWHistoryRow *> *rows;
@property (nonatomic, assign) NSInteger selected;
@property (nonatomic, assign) BOOL closing;
@property (nonatomic, strong) id escMonitor;
- (void)copyRow:(WWHistoryRow *)row;
- (void)setHoverRow:(WWHistoryRow *)row;
- (void)dismiss;
@end

static WWHistoryPanel *gPanel;

static NSTextField *wwLabel(NSFont *font, NSColor *color) {
    NSTextField *l = [NSTextField labelWithString:@""];
    l.font = font;
    l.textColor = color;
    l.drawsBackground = NO;
    l.bezeled = NO;
    l.editable = NO;
    l.selectable = NO;
    return l;
}

@implementation WWHistoryRow
- (instancetype)initWithFrame:(NSRect)frame time:(NSString *)time text:(NSString *)text {
    if ((self = [super initWithFrame:frame])) {
        _fullText = [text copy];
        _timeText = [time copy];
        CGFloat inner = frame.size.width - 2 * kPad;

        _timeLabel = wwLabel([NSFont systemFontOfSize:10 weight:NSFontWeightMedium],
                             [NSColor colorWithWhite:1 alpha:0.45]);
        _timeLabel.frame = NSMakeRect(kPad, frame.size.height - 18, inner, 13);
        _timeLabel.stringValue = time;
        [self addSubview:_timeLabel];

        NSString *oneLine = [[text componentsSeparatedByCharactersInSet:
            [NSCharacterSet newlineCharacterSet]] componentsJoinedByString:@" "];
        _textLabel = wwLabel([NSFont systemFontOfSize:12], [NSColor colorWithWhite:1 alpha:0.88]);
        _textLabel.frame = NSMakeRect(kPad, 6, inner, frame.size.height - 24);
        _textLabel.stringValue = oneLine;
        _textLabel.maximumNumberOfLines = 2;
        _textLabel.lineBreakMode = NSLineBreakByTruncatingTail;
        _textLabel.cell.wraps = YES;
        _textLabel.cell.truncatesLastVisibleLine = YES;
        [self addSubview:_textLabel];

        [self addTrackingArea:[[NSTrackingArea alloc]
            initWithRect:NSZeroRect
                 options:NSTrackingMouseEnteredAndExited | NSTrackingActiveAlways | NSTrackingInVisibleRect
                   owner:self
                userInfo:nil]];
    }
    return self;
}

- (NSView *)hitTest:(NSPoint)point {
    return NSPointInRect([self convertPoint:point fromView:self.superview], self.bounds) ? self : nil;
}

- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }

- (void)drawRect:(NSRect)dirty {
    NSColor *bg;
    if (self.copied) {
        bg = [NSColor colorWithRed:0.20 green:0.55 blue:0.32 alpha:0.45];
    } else if (self.hovered) {
        bg = [NSColor colorWithWhite:1 alpha:0.13];
    } else {
        bg = [NSColor colorWithWhite:1 alpha:0.06];
    }
    [bg set];
    [[NSBezierPath bezierPathWithRoundedRect:self.bounds xRadius:10 yRadius:10] fill];
}

- (void)refresh {
    if (self.copied) {
        self.timeLabel.stringValue = [self.timeText stringByAppendingString:@"  ✓ скопировано"];
        self.timeLabel.textColor = [NSColor colorWithRed:0.47 green:0.90 blue:0.55 alpha:1];
    } else {
        self.timeLabel.stringValue = self.timeText;
        self.timeLabel.textColor = [NSColor colorWithWhite:1 alpha:0.45];
    }
    [self setNeedsDisplay:YES];
}

- (void)mouseEntered:(NSEvent *)e { [self.panel setHoverRow:self]; }
- (void)mouseExited:(NSEvent *)e {
    if (self.hovered) [self.panel setHoverRow:nil];
}
- (void)mouseUp:(NSEvent *)e { [self.panel copyRow:self]; }
@end

@implementation WWHistoryPanel
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return NO; }

- (void)setHoverRow:(WWHistoryRow *)row {
    self.selected = row ? (NSInteger)[self.rows indexOfObject:row] : -1;
    for (WWHistoryRow *r in self.rows) {
        BOOL h = (r == row);
        if (r.hovered != h) {
            r.hovered = h;
            [r setNeedsDisplay:YES];
        }
    }
}

- (void)copyRow:(WWHistoryRow *)row {
    if (self.closing || !row) return;
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    [pb setString:row.fullText forType:NSPasteboardTypeString];
    row.copied = YES;
    [row refresh];
    self.closing = YES;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.65 * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{ [self dismiss]; });
}

- (void)keyDown:(NSEvent *)e {
    switch (e.keyCode) {
    case 53: // Esc
        [self dismiss];
        return;
    case 125: // Down
    case 126: { // Up
        NSInteger n = (NSInteger)self.rows.count;
        if (n == 0) return;
        NSInteger s = self.selected;
        if (e.keyCode == 125) s = s < 0 ? 0 : MIN(n - 1, s + 1);
        else s = s < 0 ? n - 1 : MAX(0, s - 1);
        [self setHoverRow:self.rows[s]];
        return;
    }
    case 36: // Return
    case 76: // Enter
        if (self.selected >= 0 && self.selected < (NSInteger)self.rows.count)
            [self copyRow:self.rows[self.selected]];
        return;
    }
    [super keyDown:e];
}

- (void)cancelOperation:(id)sender { [self dismiss]; }

- (void)windowDidResignKey:(NSNotification *)n {
    if (!self.closing) [self dismiss];
}

- (void)dismiss {
    if (gPanel != self) return;
    if (self.escMonitor) {
        [NSEvent removeMonitor:self.escMonitor];
        self.escMonitor = nil;
    }
    self.delegate = nil;
    [self orderOut:nil];
    gPanel = nil;
}
@end

static NSRect wwPlaceNearCursor(CGFloat w, CGFloat h) {
    NSPoint m = [NSEvent mouseLocation];
    NSScreen *screen = [NSScreen mainScreen];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSPointInRect(m, s.frame)) { screen = s; break; }
    }
    NSRect vf = screen.visibleFrame;
    CGFloat x = m.x - w / 2;
    CGFloat y = m.y - 14 - h; // below the cursor (Cocoa y grows upward)
    if (x + w > NSMaxX(vf) - 8) x = NSMaxX(vf) - w - 8;
    if (x < NSMinX(vf) + 8) x = NSMinX(vf) + 8;
    if (y < NSMinY(vf) + 8) y = m.y + 14; // no room below: open above
    if (y + h > NSMaxY(vf) - 8) y = NSMaxY(vf) - h - 8;
    return NSMakeRect(x, y, w, h);
}

// Height of a row's text block: one line for short entries, at most two.
static CGFloat wwTextHeight(NSString *text, CGFloat width) {
    NSFont *font = [NSFont systemFontOfSize:12];
    CGFloat line = ceil(font.ascender - font.descender + font.leading) + 1;
    NSString *oneLine = [[text componentsSeparatedByCharactersInSet:
        [NSCharacterSet newlineCharacterSet]] componentsJoinedByString:@" "];
    NSRect r = [oneLine boundingRectWithSize:NSMakeSize(width, CGFLOAT_MAX)
                                     options:NSStringDrawingUsesLineFragmentOrigin
                                  attributes:@{NSFontAttributeName: font}];
    return r.size.height > line * 1.5 ? line * 2 : line;
}

static void wwShow(NSArray<NSString *> *times, NSArray<NSString *> *texts) {
    NSUInteger n = texts.count;
    CGFloat rowW = kPanelW - 16;
    NSMutableArray<NSNumber *> *rowHeights = [NSMutableArray arrayWithCapacity:n];
    CGFloat rowsH = 0;
    for (NSUInteger i = 0; i < n; i++) {
        // 6 top gap + 13 time + 5 gap + text + 6 bottom, plus 4 between rows.
        CGFloat rh = 24 + wwTextHeight(texts[i], rowW - 2 * kPad - 4) + 4;
        [rowHeights addObject:@(rh)];
        rowsH += rh;
    }
    CGFloat h = kHeaderH + kPad / 2 + (n == 0 ? kRowH : rowsH);
    NSRect frame = wwPlaceNearCursor(kPanelW, h);

    WWHistoryPanel *p = [[WWHistoryPanel alloc]
        initWithContentRect:frame
                  styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                    backing:NSBackingStoreBuffered
                      defer:NO];
    p.releasedWhenClosed = NO;
    p.level = NSPopUpMenuWindowLevel;
    p.opaque = NO;
    p.backgroundColor = [NSColor clearColor];
    p.hasShadow = YES;
    p.hidesOnDeactivate = NO;
    p.becomesKeyOnlyIfNeeded = NO;
    p.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                           NSWindowCollectionBehaviorFullScreenAuxiliary |
                           NSWindowCollectionBehaviorTransient;
    p.delegate = p;
    p.selected = -1;
    p.rows = [NSMutableArray array];

    NSVisualEffectView *bg = [[NSVisualEffectView alloc] initWithFrame:NSMakeRect(0, 0, kPanelW, h)];
    bg.material = NSVisualEffectMaterialHUDWindow;
    bg.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    bg.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    bg.state = NSVisualEffectStateActive;
    bg.wantsLayer = YES;
    bg.layer.cornerRadius = 14;
    bg.layer.masksToBounds = YES;
    bg.layer.borderWidth = 1;
    bg.layer.borderColor = [NSColor colorWithWhite:1 alpha:0.10].CGColor;
    p.contentView = bg;

    NSTextField *header = wwLabel([NSFont systemFontOfSize:11 weight:NSFontWeightMedium],
                                  [NSColor colorWithWhite:1 alpha:0.5]);
    header.stringValue = @"Последние диктовки — клик копирует, Esc закрывает";
    header.frame = NSMakeRect(kPad + 4, h - kHeaderH + 6, kPanelW - 2 * kPad, 16);
    [bg addSubview:header];

    if (n == 0) {
        NSTextField *empty = wwLabel([NSFont systemFontOfSize:13], [NSColor colorWithWhite:1 alpha:0.45]);
        empty.stringValue = @"История пуста";
        empty.frame = NSMakeRect(kPad + 4, h - kHeaderH - 34, kPanelW - 2 * kPad, 18);
        [bg addSubview:empty];
    }
    CGFloat top = h - kHeaderH;
    for (NSUInteger i = 0; i < n; i++) {
        CGFloat rh = rowHeights[i].doubleValue;
        top -= rh;
        WWHistoryRow *row = [[WWHistoryRow alloc]
            initWithFrame:NSMakeRect(8, top + 2, rowW, rh - 4)
                     time:times[i]
                     text:texts[i]];
        row.panel = p;
        [bg addSubview:row];
        [p.rows addObject:row];
    }

    // Esc typed while another app keeps keyboard focus still closes the panel.
    __weak WWHistoryPanel *weakPanel = p;
    p.escMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskKeyDown
                                                          handler:^(NSEvent *e) {
        if (e.keyCode == 53) [weakPanel dismiss];
    }];

    gPanel = p;
    [p makeKeyAndOrderFront:nil];
}

void wispwind_historyToggle(char **times, char **texts, int count) {
    NSMutableArray *ts = [NSMutableArray arrayWithCapacity:count];
    NSMutableArray *xs = [NSMutableArray arrayWithCapacity:count];
    for (int i = 0; i < count; i++) {
        [ts addObject:[NSString stringWithUTF8String:times[i]] ?: @""];
        [xs addObject:[NSString stringWithUTF8String:texts[i]] ?: @""];
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gPanel) {
            [gPanel dismiss];
            return;
        }
        wwShow(ts, xs);
    });
}

void wispwind_historyClose(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [gPanel dismiss];
    });
}
*/
import "C"

import (
	"time"
	"unsafe"
)

type Item struct {
	Time time.Time
	Text string
}

const maxRows = 8

// Toggle opens the panel with the given items, or closes it if already open.
func Toggle(items []Item) {
	if len(items) > maxRows {
		items = items[:maxRows]
	}
	n := len(items)
	times := make([]*C.char, n+1)
	texts := make([]*C.char, n+1)
	for i, it := range items {
		times[i] = C.CString(it.Time.Format("15:04"))
		texts[i] = C.CString(it.Text)
	}
	C.wispwind_historyToggle(&times[0], &texts[0], C.int(n))
	for i := 0; i < n; i++ {
		C.free(unsafe.Pointer(times[i]))
		C.free(unsafe.Pointer(texts[i]))
	}
}

func Close() {
	C.wispwind_historyClose()
}

//go:build darwin

package widget

import (
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/go-vgo/robotgo"
)

/*
#cgo LDFLAGS: -framework Cocoa -framework Foundation

#import <Cocoa/Cocoa.h>

@interface OverlayView : NSView
@property (nonatomic, assign) double levelAt;
@property (nonatomic, retain) NSArray *levels;
@property (nonatomic, retain) NSString *status;
@property (nonatomic, assign) NSTimeInterval started;
@property (nonatomic, assign) BOOL wide;
@end

@implementation OverlayView
- (void)drawRect:(NSRect)dirtyRect {
    [[NSColor colorWithRed:24.0/255.0 green:24.0/255.0 blue:26.0/255.0 alpha:0.92] set];
    NSBezierPath *path = [NSBezierPath bezierPathWithRoundedRect:[self bounds] xRadius:[self bounds].size.height/2 yRadius:[self bounds].size.height/2];
    [path fill];

    if (!self.wide) {
        // Draw idle glyph
        [[NSColor colorWithRed:120.0/255.0 green:220.0/255.0 blue:130.0/255.0 alpha:1.0] set];
        double baseY = [self bounds].size.height / 2;
        int heights[] = {6, 12, 6};
        for (int i = 0; i < 3; i++) {
            NSRect rect = NSMakeRect(10 + i * 5, baseY - heights[i] / 2, 2, heights[i]);
            NSRectFill(rect);
        }
    } else {
        if ([self.status isEqualToString:@"processing"]) {
            [self drawProcessingWave];
        } else {
            [self drawWaveform];
            [self drawTimer];
        }
    }
}

- (void)drawWaveform {
    double baseY = [self bounds].size.height / 2;
    double left = 22;
    double right = [self bounds].size.width - 66;
    int barCount = 52;
    double step = (right - left) / barCount;

    for (int i = 0; i < barCount; i++) {
        int idx = ((int)self.levelAt - barCount + i + (int)self.levels.count) % (int)self.levels.count;
        double lvl = [[self.levels objectAtIndex:idx] doubleValue];
        if (lvl < 0.008) continue;

        // Smooth
        double prev = [[self.levels objectAtIndex:(idx-1+(int)self.levels.count)%(int)self.levels.count] doubleValue];
        double next = [[self.levels objectAtIndex:(idx+1)%(int)self.levels.count] doubleValue];
        lvl = prev*0.25 + lvl*0.5 + next*0.25;

        lvl = fmax(0.08, fmin(1.0, lvl * 7.0));
        double barH = 4 + lvl * 22;

        double r = 30 + lvl * 100;
        double g = 150 + lvl * 105;
        double b = 50 + lvl * 50;
        [[NSColor colorWithRed:r/255.0 green:g/255.0 blue:b/255.0 alpha:1.0] set];

        NSRect rect = NSMakeRect(left + i * step, baseY - barH / 2, 3, barH);
        NSBezierPath *p = [NSBezierPath bezierPathWithRoundedRect:rect xRadius:1.5 yRadius:1.5];
        [p fill];
    }
}

- (void)drawProcessingWave {
    double baseY = [self bounds].size.height / 2;
    double left = 26;
    double right = [self bounds].size.width - 26;
    int barCount = 48;
    double step = (right - left) / barCount;
    double elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    double phase = elapsed * 4.2;

    for (int i = 0; i < barCount; i++) {
        double x = left + i * step;
        double t = i * 0.36 - phase;
        double lvl = 0.18 + 0.82 * (sin(t) + 1) / 2;
        double envelope = sin((double)i / (barCount - 1) * M_PI);
        double barH = 4 + lvl * envelope * 24;

        double pulse = 0.5 + 0.5 * sin(t * 0.7);
        double r = 40 + pulse * 80;
        double g = 180 + pulse * 75;
        double b = 60 + pulse * 40;
        [[NSColor colorWithRed:r/255.0 green:g/255.0 blue:b/255.0 alpha:1.0] set];

        NSRect rect = NSMakeRect(x, baseY - barH / 2, 3, barH);
        NSBezierPath *p = [NSBezierPath bezierPathWithRoundedRect:rect xRadius:1.5 yRadius:1.5];
        [p fill];
    }
}

- (void)drawTimer {
    double elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    int totalSeconds = (int)elapsed;
    NSString *timeStr = [NSString stringWithFormat:@"%d:%02d", totalSeconds / 60, totalSeconds % 60];

    NSDictionary *attrs = @{
        NSFontAttributeName: [NSFont systemFontOfSize:13],
        NSForegroundColorAttributeName: [NSColor colorWithRed:165.0/255.0 green:165.0/255.0 blue:165.0/255.0 alpha:1.0]
    };
    [timeStr drawAtPoint:NSMakePoint([self bounds].size.width - 44, 14) withAttributes:attrs];
}
@end

static NSWindow *window;
static OverlayView *view;

void initWindow() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSRect frame = NSMakeRect(0, 0, 32, 24);
        window = [[NSWindow alloc] initWithContentRect:frame
                                             styleMask:NSWindowStyleMaskBorderless
                                               backing:NSBackingStoreBuffered
                                                 defer:NO];
        [window setBackgroundColor:[NSColor clearColor]];
        [window setOpaque:NO];
        [window setHasShadow:NO];
        [window setLevel:NSStatusWindowLevel];
        [window setIgnoresMouseEvents:YES];
        [window setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary];

        view = [[OverlayView alloc] initWithFrame:frame];
        [window setContentView:view];
    });
}

void hideWindow() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [window orderOut:nil];
    });
}

void updateWindow(int x, int y, int w, int h, bool wide, bool visible, const char *status, double started, double *levels, int levelCount, int levelAt) {
    NSString *statusStr = [NSString stringWithUTF8String:status];
    NSMutableArray *levelsArr = [NSMutableArray arrayWithCapacity:levelCount];
    for (int i = 0; i < levelCount; i++) {
        [levelsArr addObject:[NSNumber numberWithDouble:levels[i]]];
    }

    dispatch_async(dispatch_get_main_queue(), ^{
        if (!visible) {
            [window orderOut:nil];
            return;
        }

        [window setFrame:NSMakeRect(x, y, w, h) display:YES];
        view.wide = wide;
        view.status = statusStr;
        view.started = started;
        view.levelAt = levelAt;
        view.levels = levelsArr;

        [window orderFrontRegardless];
        [view setNeedsDisplay:YES];
    });
}
*/
import "C"

const (
	compactWidth  = 32
	compactHeight = 24
	wideWidth     = 360
	wideHeight    = 44
)

var overlay = &state{
	levels: make([]float64, 80),
	status: "idle",
}

type state struct {
	mu      sync.Mutex
	levels  []float64
	levelAt int
	status  string
	started time.Time
	visible bool
	wide    bool
	width   int32
	height  int32
	ready   chan struct{}
	stopCh  chan struct{}
}

func Start() {
	overlay.mu.Lock()
	if overlay.ready != nil {
		overlay.mu.Unlock()
		return
	}
	overlay.ready = make(chan struct{})
	overlay.stopCh = make(chan struct{})
	overlay.mu.Unlock()

	C.initWindow()
	close(overlay.ready)

	go func() {
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				overlay.mu.Lock()
				if overlay.visible {
					mx, my := robotgo.GetMousePos()
					x := mx + 16
					y := my + 16
					_, sh := robotgo.GetScreenSize()
					y = sh - y - int(overlay.height)

					status := C.CString(overlay.status)
					C.updateWindow(
						C.int(x), C.int(y), C.int(overlay.width), C.int(overlay.height),
						C.bool(overlay.wide), C.bool(overlay.visible),
						status,
						C.double(overlay.started.UnixNano())/1e9,
						(*C.double)(&overlay.levels[0]), C.int(len(overlay.levels)), C.int(overlay.levelAt),
					)
					C.free(unsafe.Pointer(status))
				}
				overlay.mu.Unlock()
			case <-overlay.stopCh:
				return
			}
		}
	}()
}

func ShowIdle() {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.status = "idle"
	overlay.visible = true
	overlay.wide = false
	overlay.width = compactWidth
	overlay.height = compactHeight
	overlay.mu.Unlock()
}

func Show(status string) {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.status = status
	overlay.started = time.Now()
	overlay.visible = true
	overlay.wide = true
	overlay.width = wideWidth
	overlay.height = wideHeight
	for i := range overlay.levels {
		overlay.levels[i] = 0
	}
	overlay.mu.Unlock()
}

func Hide() {
	Start()
	<-overlay.ready
	overlay.mu.Lock()
	overlay.visible = false
	overlay.mu.Unlock()
	C.hideWindow()
}

func SetStatus(status string) {
	overlay.mu.Lock()
	overlay.status = status
	overlay.mu.Unlock()
}

func SetLevel(level float64) {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	overlay.mu.Lock()
	overlay.levels[overlay.levelAt] = level
	overlay.levelAt = (overlay.levelAt + 1) % len(overlay.levels)
	overlay.mu.Unlock()
}

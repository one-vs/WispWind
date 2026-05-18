//go:build darwin

package widget

import (
	"sync"
	"time"
	"unsafe"

	"github.com/go-vgo/robotgo"
)

/*
#cgo LDFLAGS: -framework Cocoa -framework Foundation -framework QuartzCore
#cgo CFLAGS: -x objective-c

#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

@interface OverlayView : NSView
@property (nonatomic, assign) double levelAt;
@property (nonatomic, retain) NSArray *levels;
@property (nonatomic, retain) NSString *status;
@property (nonatomic, assign) NSTimeInterval started;
@property (nonatomic, assign) BOOL wide;
@end

@implementation OverlayView
- (void)drawRect:(NSRect)dirtyRect {
    [[NSColor colorWithRed:10.0/255.0 green:10.0/255.0 blue:12.0/255.0 alpha:0.95] set];
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
            [self drawSiriWave];
            [self drawTimer];
        }
    }
}

- (void)drawSiriWave {
    double width = [self bounds].size.width;
    double height = [self bounds].size.height;
    double baseY = height / 2;
    
    // Get current volume level (average of last few samples)
    double avgLevel = 0;
    int count = 8;
    for (int i = 0; i < count; i++) {
        int idx = ((int)self.levelAt - 1 - i + (int)self.levels.count) % (int)self.levels.count;
        avgLevel += [[self.levels objectAtIndex:idx] doubleValue];
    }
    avgLevel /= count;
    
    double sensitivity = 15.0;
    double normalizedLevel = fmax(0.01, fmin(1.0, avgLevel * sensitivity));
    BOOL isSilent = (normalizedLevel < 0.04);

    NSTimeInterval elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    
    // Draw 7 overlapping waves for a richer look
    struct Wave {
        double freq;
        double amp;
        double phase;
        NSColor *color;
        double width;
    } waves[] = {
        {1.1, 0.8, elapsed * 4.5, [NSColor colorWithRed:0.0 green:0.9 blue:1.0 alpha:0.8], 3.0},  // Cyan
        {0.7, 0.6, elapsed * 2.8, [NSColor colorWithRed:0.7 green:0.2 blue:1.0 alpha:0.7], 2.5},  // Purple
        {1.4, 0.4, elapsed * 5.5, [NSColor colorWithRed:1.0 green:0.1 blue:0.5 alpha:0.6], 2.0},  // Pink
        {0.9, 0.5, elapsed * 3.2, [NSColor colorWithRed:0.2 green:1.0 blue:0.4 alpha:0.5], 2.0},  // Green
        {1.8, 0.3, elapsed * 6.0, [NSColor colorWithRed:1.0 green:0.8 blue:0.0 alpha:0.4], 1.5},  // Gold
        {0.5, 0.7, elapsed * 2.0, [NSColor colorWithRed:0.0 green:0.4 blue:1.0 alpha:0.5], 2.5},  // Blue
        {2.2, 0.2, elapsed * 7.5, [NSColor colorWithRed:1.0 green:1.0 blue:1.0 alpha:0.3], 1.0}   // White
    };

    if (isSilent) {
        // Draw a single faint idle line
        [[NSColor colorWithWhite:0.5 alpha:0.2] set];
        NSBezierPath *p = [NSBezierPath bezierPath];
        [p moveToPoint:NSMakePoint(35, baseY)];
        [p lineToPoint:NSMakePoint(width - 90, baseY)];
        p.lineWidth = 1.0;
        [p stroke];
        return;
    }

    // High volume "flash" effect
    if (normalizedLevel > 0.6) {
        [[NSColor colorWithWhite:1.0 alpha:(normalizedLevel - 0.6) * 0.3] set];
        NSBezierPath *flash = [NSBezierPath bezierPathWithRoundedRect:[self bounds] xRadius:height/2 yRadius:height/2];
        [flash fill];
    }

    for (int i = 0; i < 7; i++) {
        struct Wave wave = waves[i];
        NSBezierPath *p = [NSBezierPath bezierPath];
        [wave.color set];
        
        double left = 35;
        double right = width - 90;
        double waveWidth = right - left;
        
        [p moveToPoint:NSMakePoint(left, baseY)];
        
        for (double x = 0; x <= waveWidth; x += 1.0) {
            double normX = x / waveWidth;
            double envelope = pow(sin(normX * M_PI), 2.5);
            
            double y = baseY + sin(x * 0.07 * wave.freq + wave.phase) * (height * 0.45) * normalizedLevel * envelope * wave.amp;
            [p lineToPoint:NSMakePoint(left + x, y)];
        }
        
        p.lineWidth = wave.width;
        p.lineCapStyle = NSLineCapStyleRound;
        [p stroke];
    }
}

- (void)drawProcessingWave {
    double width = [self bounds].size.width;
    double height = [self bounds].size.height;
    double baseY = height / 2;
    NSTimeInterval elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    
    double left = 35;
    double right = width - 35;
    double waveWidth = right - left;

    NSColor *purple = [NSColor colorWithRed:0.7 green:0.3 blue:1.0 alpha:1.0];
    
    for (int i = 0; i < 3; i++) {
        NSBezierPath *p = [NSBezierPath bezierPath];
        double alpha = 0.8 / (i + 1);
        double lineWidth = 2.0 + (i * 4.0);
        [[purple colorWithAlphaComponent:alpha] set];
        
        [p moveToPoint:NSMakePoint(left, baseY)];
        for (double x = 0; x <= waveWidth; x += 1.0) {
            double normX = x / waveWidth;
            double envelope = sin(normX * M_PI);
            double y = baseY + sin(x * 0.12 - elapsed * 18.0) * 12.0 * envelope;
            [p lineToPoint:NSMakePoint(left + x, y)];
        }
        p.lineWidth = lineWidth;
        p.lineCapStyle = NSLineCapStyleRound;
        [p stroke];
    }
}

- (void)drawTimer {
    double elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    int totalSeconds = (int)elapsed;
    NSString *timeStr = [NSString stringWithFormat:@"%d:%02d", totalSeconds / 60, totalSeconds % 60];

    // Vertically center text: (height - fontHeight) / 2
    // For 14pt font, fontHeight is roughly 16-18pt. 
    // (48 - 16) / 2 = 16.
    double textY = 16; 

    NSDictionary *attrs = @{
        NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:14 weight:NSFontWeightBold],
        NSForegroundColorAttributeName: [NSColor whiteColor]
    };
    [timeStr drawAtPoint:NSMakePoint([self bounds].size.width - 65, textY) withAttributes:attrs];
}
@end

static NSWindow *window;
static OverlayView *view;

void wispwind_initWindow() {
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

void wispwind_hideWindow() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [window orderOut:nil];
    });
}

void wispwind_updateWindow(int x, int y, int w, int h, bool wide, bool visible, const char *status, double started, double *levels, int levelCount, int levelAt) {
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
	wideWidth     = 380
	wideHeight    = 48
)

var overlay = &state{
	levels: make([]float64, 80),
	status: "idle",
}

type state struct {
	mu       sync.Mutex
	levels   []float64
	levelAt  int
	status   string
	started  time.Time
	visible  bool
	wide     bool
	width    int32
	height   int32
	anchorX  int
	anchorY  int
	anchored bool
	ready    chan struct{}
	stopCh   chan struct{}
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

	C.wispwind_initWindow()
	close(overlay.ready)

	go func() {
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				overlay.mu.Lock()
				if overlay.visible {
					x := overlay.anchorX
					y := overlay.anchorY

					status := C.CString(overlay.status)
					C.wispwind_updateWindow(
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

	mx, my := robotgo.GetMousePos()
	sw, sh := robotgo.GetScreenSize()

	const gap = 10
	const edgePad = 12

	w := int(wideWidth)
	h := int(wideHeight)

	anchorX := mx - w/2
	if anchorX+w > sw-edgePad {
		anchorX = sw - w - edgePad
	}
	if anchorX < edgePad {
		anchorX = edgePad
	}

	anchorY := sh - my - gap - h
	if anchorY < edgePad {
		anchorY = sh - my + gap
	}
	if anchorY+h > sh-edgePad {
		anchorY = sh - h - edgePad
	}

	overlay.mu.Lock()
	overlay.status = status
	overlay.started = time.Now()
	overlay.visible = true
	overlay.wide = true
	overlay.width = wideWidth
	overlay.height = wideHeight
	overlay.anchorX = anchorX
	overlay.anchorY = anchorY
	overlay.anchored = true
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
	overlay.anchored = false
	overlay.mu.Unlock()
	C.wispwind_hideWindow()
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

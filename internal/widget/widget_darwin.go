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

// Siri-style voice visualization: several luminous colored curves, each made of
// a few drifting "blobs" (gaussian-windowed sines). Blobs are mirrored around the
// centre line, filled translucently and composited additively so overlaps glow.

#define WW_CURVES 3
#define WW_BLOBS 4

typedef struct {
    double offset;   // centre, in [-1, 1]
    double width;    // gaussian width
    double freq;     // sine frequency
    double phase;
    double speed;    // phase velocity
    double drift;    // offset velocity
    double amp;      // relative height
    double born;
    double life;
} WWBlob;

static double wwRand(double lo, double hi) {
    return lo + (hi - lo) * ((double)arc4random_uniform(1000000) / 1000000.0);
}

static void wwSpawn(WWBlob *b, double now, BOOL initial) {
    b->offset = wwRand(-0.75, 0.75);
    b->width = wwRand(0.18, 0.45);
    b->freq = wwRand(2.5, 6.0);
    b->phase = wwRand(0, 2 * M_PI);
    b->speed = wwRand(-9.0, 9.0);
    b->drift = wwRand(-0.25, 0.25);
    b->amp = wwRand(0.45, 1.0);
    b->life = wwRand(1.2, 2.8);
    // Stagger initial blobs so they don't all respawn together.
    b->born = initial ? now - wwRand(0, b->life) : now;
}

@interface OverlayView : NSView {
    WWBlob blobs[WW_CURVES][WW_BLOBS];
    BOOL seeded;
    double lastTime;
    double amp;        // smoothed voice amplitude, 0..1
    double hue;        // slow color rotation
}
@property (nonatomic, assign) double levelAt;
@property (nonatomic, retain) NSArray *levels;
@property (nonatomic, retain) NSString *status;
@property (nonatomic, assign) NSTimeInterval started;
@property (nonatomic, assign) BOOL wide;
@end

@implementation OverlayView
- (BOOL)isOpaque { return NO; }

- (void)drawRect:(NSRect)dirtyRect {
    NSRect b = [self bounds];
    CGContextRef ctx = [[NSGraphicsContext currentContext] CGContext];
    CGFloat r = b.size.height / 2;

    CGContextClearRect(ctx, b);

    NSBezierPath *capsule = [NSBezierPath bezierPathWithRoundedRect:b xRadius:r yRadius:r];
    NSGradient *bg = [[NSGradient alloc] initWithStartingColor:[NSColor colorWithRed:0.07 green:0.07 blue:0.10 alpha:0.94]
                                                   endingColor:[NSColor colorWithRed:0.02 green:0.02 blue:0.04 alpha:0.94]];
    [bg drawInBezierPath:capsule angle:-90];
    [bg release];

    if (!self.wide) {
        [self drawIdleGlyph];
    } else {
        CGContextSaveGState(ctx);
        [capsule addClip];
        [self drawSiriWave:ctx];
        CGContextRestoreGState(ctx);
        if (![self.status isEqualToString:@"processing"]) {
            [self drawTimer];
        }
    }

    // Hairline rim to lift the capsule off dark backgrounds.
    NSBezierPath *rim = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(b, 0.5, 0.5) xRadius:r - 0.5 yRadius:r - 0.5];
    [[NSColor colorWithWhite:1.0 alpha:0.10] set];
    rim.lineWidth = 1.0;
    [rim stroke];
}

- (void)drawIdleGlyph {
    NSRect b = [self bounds];
    NSPoint c = NSMakePoint(NSMidX(b), NSMidY(b));
    double pulse = 0.5 + 0.5 * sin(CACurrentMediaTime() * 2.0);
    NSGradient *g = [[NSGradient alloc] initWithColorsAndLocations:
        [NSColor colorWithRed:0.55 green:0.85 blue:1.0 alpha:0.95], 0.0,
        [NSColor colorWithRed:0.55 green:0.35 blue:1.0 alpha:0.55], 0.55,
        [NSColor colorWithRed:0.55 green:0.35 blue:1.0 alpha:0.0], 1.0, nil];
    [g drawFromCenter:c radius:0 toCenter:c radius:6 + pulse * 2 options:0];
    [g release];
}

- (double)targetAmplitude {
    NSUInteger n = self.levels.count;
    if (n == 0) return 0;
    double sum = 0;
    int count = 3;
    for (int i = 0; i < count; i++) {
        int idx = ((int)self.levelAt - 1 - i + (int)n) % (int)n;
        sum += [[self.levels objectAtIndex:idx] doubleValue];
    }
    double rms = sum / count;
    if (rms <= 0.0001) return 0;
    // Map loudness in dB (-50..-12 dBFS) to 0..1 for a perceptual response.
    double db = 20.0 * log10(rms);
    return fmax(0, fmin(1, (db + 50.0) / 38.0));
}

- (void)drawSiriWave:(CGContextRef)ctx {
    double now = CACurrentMediaTime();
    double dt = lastTime > 0 ? fmin(0.1, now - lastTime) : 1.0 / 60.0;
    lastTime = now;

    if (!seeded) {
        for (int c = 0; c < WW_CURVES; c++)
            for (int i = 0; i < WW_BLOBS; i++)
                wwSpawn(&blobs[c][i], now, YES);
        seeded = YES;
    }

    BOOL processing = [self.status isEqualToString:@"processing"];
    double target;
    double speedMul;
    if (processing) {
        // Energetic "thinking" state while audio is sent to the model.
        target = 0.82 + 0.12 * sin(now * 5.0);
        speedMul = 1.3;
    } else {
        target = [self targetAmplitude];
        speedMul = 0.35 + 1.1 * amp;
    }
    // Fast attack, slower release, frame-rate independent.
    double rate = target > amp ? 22.0 : 7.0;
    amp += (target - amp) * (1.0 - exp(-rate * dt));
    hue += dt * (processing ? 0.6 : 0.05 + amp * 0.25);
    // Processing: a light sweeps back and forth, lighting the waves as it passes.
    double scan = sin(now * 2.6);

    NSRect bounds = [self bounds];
    double width = bounds.size.width;
    double height = bounds.size.height;
    double midY = height / 2;
    double left = 18;
    double right = processing ? width - 18 : width - 64;
    double span = right - left;
    double maxH = height / 2 - 3;

    // Ambient glow behind the waves, breathing with the voice.
    {
        CGFloat cx = left + span / 2;
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGFloat comps[] = {
            0.35, 0.40, 1.0, 0.08 + 0.20 * amp,
            0.35, 0.40, 1.0, 0.0
        };
        CGFloat locs[] = {0, 1};
        CGGradientRef g = CGGradientCreateWithColorComponents(cs, comps, locs, 2);
        CGContextSaveGState(ctx);
        CGContextTranslateCTM(ctx, cx, midY);
        CGContextScaleCTM(ctx, span / height, 1.0);
        CGContextDrawRadialGradient(ctx, g, CGPointZero, 0, CGPointZero, height * 0.9, 0);
        CGContextRestoreGState(ctx);
        CGGradientRelease(g);
        CGColorSpaceRelease(cs);
    }

    // Palette: cyan, magenta, violet — rotated slightly over time.
    double base[WW_CURVES][3] = {
        {0.10, 0.85, 1.00},
        {1.00, 0.22, 0.62},
        {0.48, 0.32, 1.00},
    };

    CGContextSaveGState(ctx);
    CGContextSetBlendMode(ctx, kCGBlendModePlusLighter);

    // Keep a visible shimmer even in silence, like Siri's resting line.
    double level = fmax(0.06, amp);
    int step = 2;

    for (int c = 0; c < WW_CURVES; c++) {
        double shift = 0.5 + 0.5 * sin(hue * 2 * M_PI + c * 2.1);
        double cr = base[c][0] * (0.8 + 0.2 * shift);
        double cg = base[c][1] * (0.8 + 0.2 * (1 - shift));
        double cb = base[c][2];

        int pts = (int)(span / step) + 1;
        double ys[pts];
        double peak = 0;
        for (int k = 0; k < pts; k++) {
            double x = -1.0 + 2.0 * k / (pts - 1);
            double y = 0;
            for (int i = 0; i < WW_BLOBS; i++) {
                WWBlob *bl = &blobs[c][i];
                double t = (now - bl->born) / bl->life;
                double fade = sin(fmin(1, fmax(0, t)) * M_PI);
                double d = (x - bl->offset) / bl->width;
                y += bl->amp * fade * exp(-d * d) * sin(bl->freq * x * M_PI - bl->phase);
            }
            double edge = 1 - x * x;
            y = fabs(y) * edge * edge;
            if (processing) {
                double ds = x - scan;
                y *= 0.3 + 0.9 * exp(-ds * ds / 0.12);
            }
            ys[k] = y;
            if (y > peak) peak = y;
        }
        double norm = peak > 1 ? 1.0 / peak : 1.0;

        CGMutablePathRef path = CGPathCreateMutable();
        CGPathMoveToPoint(path, NULL, left, midY);
        for (int k = 0; k < pts; k++)
            CGPathAddLineToPoint(path, NULL, left + k * step, midY + ys[k] * norm * maxH * level);
        for (int k = pts - 1; k >= 0; k--)
            CGPathAddLineToPoint(path, NULL, left + k * step, midY - ys[k] * norm * maxH * level);
        CGPathCloseSubpath(path);

        CGColorRef glow = CGColorCreateGenericRGB(cr, cg, cb, 0.55);
        CGContextSaveGState(ctx);
        CGContextSetShadowWithColor(ctx, CGSizeZero, 6 + 10 * amp, glow);
        CGContextSetRGBFillColor(ctx, cr, cg, cb, 0.36);
        CGContextAddPath(ctx, path);
        CGContextFillPath(ctx);
        CGContextRestoreGState(ctx);

        // Bright inner core for a luminous edge.
        CGContextSetRGBStrokeColor(ctx, fmin(1, cr + 0.15), fmin(1, cg + 0.15), fmin(1, cb + 0.15), 0.3);
        CGContextSetLineWidth(ctx, 0.8);
        CGContextAddPath(ctx, path);
        CGContextStrokePath(ctx);

        CGColorRelease(glow);
        CGPathRelease(path);

        // Advance blobs.
        for (int i = 0; i < WW_BLOBS; i++) {
            WWBlob *bl = &blobs[c][i];
            bl->phase += bl->speed * speedMul * dt;
            bl->offset += bl->drift * speedMul * dt;
            if (now - bl->born > bl->life) wwSpawn(bl, now, NO);
        }
    }

    // Sweeping comet of light for the processing state.
    if (processing) {
        CGFloat cx = left + (scan + 1) / 2 * span;
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        double s = 0.5 + 0.5 * sin(hue * 2 * M_PI);
        CGFloat comps[] = {
            1.0, 1.0, 1.0, 0.6,
            0.3 + 0.7 * s, 0.6 * (1 - s) + 0.2, 1.0, 0.38,
            0.4, 0.3, 1.0, 0.0
        };
        CGFloat locs[] = {0, 0.25, 1};
        CGGradientRef g = CGGradientCreateWithColorComponents(cs, comps, locs, 3);
        CGContextSaveGState(ctx);
        CGContextTranslateCTM(ctx, cx, midY);
        CGContextScaleCTM(ctx, 3.0, 1.0);
        CGContextDrawRadialGradient(ctx, g, CGPointZero, 0, CGPointZero, height * 0.42, 0);
        CGContextRestoreGState(ctx);
        CGGradientRelease(g);
        CGColorSpaceRelease(cs);
    }

    // Thin white centre line fading toward the edges.
    {
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGFloat a = 0.18 + 0.2 * amp;
        CGFloat comps[] = {1, 1, 1, 0, 1, 1, 1, a, 1, 1, 1, 0};
        CGFloat locs[] = {0, 0.5, 1};
        CGGradientRef g = CGGradientCreateWithColorComponents(cs, comps, locs, 3);
        CGContextSaveGState(ctx);
        CGContextClipToRect(ctx, CGRectMake(left, midY - 0.5, span, 1.0));
        CGContextDrawLinearGradient(ctx, g, CGPointMake(left, midY), CGPointMake(right, midY), 0);
        CGContextRestoreGState(ctx);
        CGGradientRelease(g);
        CGColorSpaceRelease(cs);
    }

    CGContextRestoreGState(ctx);
}

- (void)drawTimer {
    double elapsed = [[NSDate date] timeIntervalSince1970] - self.started;
    int totalSeconds = (int)elapsed;
    NSString *timeStr = [NSString stringWithFormat:@"%d:%02d", totalSeconds / 60, totalSeconds % 60];

    NSDictionary *attrs = @{
        NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:13 weight:NSFontWeightSemibold],
        NSForegroundColorAttributeName: [NSColor colorWithWhite:1.0 alpha:0.85]
    };
    NSSize sz = [timeStr sizeWithAttributes:attrs];
    NSRect b = [self bounds];
    [timeStr drawAtPoint:NSMakePoint(b.size.width - 22 - sz.width, (b.size.height - sz.height) / 2) withAttributes:attrs];
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

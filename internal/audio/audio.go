package audio

import (
	"bytes"
	"encoding/binary"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/gordonklaus/portaudio"
)

const (
	SampleRate      = 24000
	framesPerBuffer = 1024
)

var (
	stream    *portaudio.Stream
	buffer    []int16
	recording bool
	onAudio   func([]int16)
	mu        sync.Mutex

	// Gain control. "auto" runs a simple AGC toward targetRMS; a number
	// fixes the gain (legacy behavior was a hardcoded 3.0).
	gainAuto  = true
	fixedGain = 3.0
	agcGain   = 3.0
)

const (
	targetRMS  = 0.15
	agcMinGain = 1.0
	agcMaxGain = 10.0
	noiseFloor = 0.004 // raw RMS below this is treated as silence, no adaptation
	agcAttack  = 0.35  // fast when reducing gain (avoid clipping)
	agcRelease = 0.06  // slow when raising gain
)

// SetGain configures gain handling: "auto" enables AGC, a numeric string
// sets a fixed gain.
func SetGain(mode string) {
	mu.Lock()
	defer mu.Unlock()
	mode = strings.TrimSpace(strings.ToLower(mode))
	if v, err := strconv.ParseFloat(mode, 64); err == nil && v > 0 {
		gainAuto = false
		fixedGain = math.Min(v, agcMaxGain)
		return
	}
	gainAuto = true
	agcGain = 3.0
}

func Init() error {
	return portaudio.Initialize()
}

func Close() {
	portaudio.Terminate()
}

func StartRecording(cb func([]int16)) error {
	mu.Lock()
	defer mu.Unlock()

	if recording {
		return nil
	}

	// PortAudio caches the device list at Initialize time: if the default
	// microphone changed since startup (headset plugged in, Bluetooth
	// reconnect), OpenDefaultStream would keep opening the stale device and
	// record pure silence. Re-enumerate devices before every recording.
	portaudio.Terminate()
	if err := portaudio.Initialize(); err != nil {
		return err
	}

	onAudio = cb
	buffer = make([]int16, 0)
	in := make([]int16, framesPerBuffer)

	var err error
	stream, err = portaudio.OpenDefaultStream(1, 0, float64(SampleRate), len(in), func(inBuf []int16) {
		mu.Lock()
		if !recording {
			mu.Unlock()
			return
		}

		// Pick the gain: AGC adapts toward targetRMS on voiced chunks,
		// dropping fast (to avoid clipping) and rising slowly.
		gain := fixedGain
		if gainAuto {
			raw := CalculateRMS(inBuf)
			if raw > noiseFloor {
				desired := math.Max(agcMinGain, math.Min(agcMaxGain, targetRMS/raw))
				k := agcRelease
				if desired < agcGain {
					k = agcAttack
				}
				agcGain += (desired - agcGain) * k
			}
			gain = agcGain
		}

		for i := range inBuf {
			val := float64(inBuf[i]) * gain
			if val > 32767 {
				val = 32767
			} else if val < -32768 {
				val = -32768
			}
			inBuf[i] = int16(val)
		}

		buffer = append(buffer, inBuf...)
		cb := onAudio
		chunk := append([]int16(nil), inBuf...)
		mu.Unlock()

		if cb != nil {
			cb(chunk)
		}
	})
	if err != nil {
		return err
	}

	recording = true
	if err := stream.Start(); err != nil {
		recording = false
		return err
	}

	return nil
}

func StopRecording() []byte {
	mu.Lock()
	defer mu.Unlock()

	if !recording {
		return nil
	}
	recording = false

	if stream != nil {
		stream.Stop()
		stream.Close()
		stream = nil
	}

	if len(buffer) == 0 {
		return nil
	}

	// Warn when the whole take was silence — almost always a dead/wrong
	// input device rather than an actually quiet user.
	if rms := CalculateRMS(buffer); rms < 0.001 {
		log.Printf("Audio warning: recording is silent (RMS %.5f) — check the default microphone", rms)
	}

	return createWAV(buffer, SampleRate)
}

func CalculateRMS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, s := range pcm {
		v := float64(s) / 32768.0
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func createWAV(pcm []int16, sRate int) []byte {
	buf := new(bytes.Buffer)

	buf.WriteString("RIFF")
	dataSize := len(pcm) * 2
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(buf, binary.LittleEndian, uint16(1)) // Mono
	binary.Write(buf, binary.LittleEndian, uint32(sRate))
	binary.Write(buf, binary.LittleEndian, uint32(sRate*2))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint16(16))

	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	for _, sample := range pcm {
		binary.Write(buf, binary.LittleEndian, sample)
	}

	return buf.Bytes()
}

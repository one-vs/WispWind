package audio

import (
	"bytes"
	"encoding/binary"
	"math"
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
)

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

	onAudio = cb
	buffer = make([]int16, 0)
	in := make([]int16, framesPerBuffer)

	const inputGain = 3.0 // Developer-only: boost mic sensitivity

	var err error
	stream, err = portaudio.OpenDefaultStream(1, 0, float64(SampleRate), len(in), func(inBuf []int16) {
		mu.Lock()
		if !recording {
			mu.Unlock()
			return
		}

		for i := range inBuf {
			val := float32(inBuf[i]) * inputGain
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

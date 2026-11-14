package audio

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// OpenEarlyMedia is called only after a call-bound authenticated lease. It
// shares the duplex reservation but never opens aplay or feeds an AP uplink.
// A physical early-media source is NOT enabled by merely having this adapter.
func (b *Bridge) OpenEarlyMedia(parent context.Context) (io.ReadCloser, error) {
	return b.openEarlyMedia(parent, startMU300EarlyCapture)
}

type earlyCaptureStart func(context.Context) (io.ReadCloser, func(), error)
type earlyCaptureStream struct {
	reader *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *earlyCaptureStream) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *earlyCaptureStream) Close() error {
	s.once.Do(func() { s.cancel(); s.reader.Close() })
	<-s.done
	return nil
}
func (b *Bridge) openEarlyMedia(parent context.Context, start earlyCaptureStart) (io.ReadCloser, error) {
	if !b.busy.CompareAndSwap(false, true) {
		return nil, errors.New("audio already owned")
	}
	downGain, _ := b.streamGains()
	ctx, cancel := context.WithCancel(parent)
	r, w := io.Pipe()
	stream := &earlyCaptureStream{reader: r, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(stream.done)
		defer b.busy.Store(false)
		defer cancel()
		defer w.Close()
		captured, cleanup, err := start(ctx)
		if err != nil {
			w.CloseWithError(errors.New("early capture unavailable"))
			return
		}
		defer cleanup() // waits for process exit before releasing DSP/scene
		defer captured.Close()
		defer cancel()
		watchDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				captured.Close()
				r.Close()
			case <-watchDone:
			}
		}()
		defer close(watchDone)
		raw := make([]byte, 960*4)
		down := NewDownsampler()
		for {
			if _, err = io.ReadFull(captured, raw); err != nil {
				w.CloseWithError(errors.New("early capture ended"))
				return
			}
			if ctx.Err() != nil {
				return
			}
			frame := down.Convert(raw)
			b.DownlinkLimited.Add(downGain.apply(frame))
			if err = writeFull(w, frame); err != nil {
				return
			}
			b.TX.Add(1)
		}
	}()
	return stream, nil
}

func startMU300EarlyCapture(ctx context.Context) (io.ReadCloser, func(), error) {
	if err := CheckTools(); err != nil {
		return nil, nil, err
	}
	devices, err := Discover()
	if err != nil {
		return nil, nil, err
	}
	stop := func() {
		clean, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = run(clean, "/usr/bin/mu300-voice", "audio", "stop")
	}
	if err = run(ctx, "/usr/bin/mu300-voice", "audio", "start", "dsp"); err != nil {
		stop()
		return nil, nil, err
	}
	if err = run(ctx, "amixer", "-q", "-c", strconv.Itoa(devices.Card), "cset", "name=agdsp_access_en", "1"); err != nil {
		stop()
		return nil, nil, err
	}
	// Same validated DAC1 downlink tap as duplex. Alerting-phase usability still
	// needs separate real-call acceptance before the gateway may advertise it.
	capture := exec.CommandContext(ctx, "arecord", "--fatal-errors", "-q", "-D", devices.Capture, "-t", "raw", "-f", "S16_LE", "-r", "48000", "-c", "2", "--period-size=960", "--buffer-size=5760")
	reader, err := capture.StdoutPipe()
	if err != nil {
		stop()
		return nil, nil, err
	}
	if err = capture.Start(); err != nil {
		reader.Close()
		stop()
		return nil, nil, err
	}
	return reader, func() { _ = capture.Wait(); stop() }, nil
}

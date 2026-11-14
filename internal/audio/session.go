package audio

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const FrameBytes = 320

type Bridge struct {
	busy            atomic.Bool
	RX              atomic.Uint64
	TX              atomic.Uint64
	Silence         atomic.Uint64
	Received        atomic.Uint64
	Dropped         atomic.Uint64
	DownlinkLimited atomic.Uint64
	UplinkLimited   atomic.Uint64
	mu              sync.Mutex
	lastError       string
	downlinkDB      int
	uplinkDB        int
}

func (b *Bridge) Status() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]any{"active": b.busy.Load(), "uplink_frames": b.RX.Load(), "downlink_frames": b.TX.Load(), "silence_frames": b.Silence.Load(), "received_uplink_frames": b.Received.Load(), "dropped_uplink_frames": b.Dropped.Load(), "last_error": b.lastError,
		"downlink_gain_db": b.downlinkDB, "uplink_gain_db": b.uplinkDB,
		"downlink_limited_samples": b.DownlinkLimited.Load(), "uplink_limited_samples": b.UplinkLimited.Load()}
}
func (b *Bridge) Handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReaderSize(c, 4096)
	hello := make([]byte, len("AIRSIMPCM1\n"))
	_, e := io.ReadFull(r, hello)
	if e != nil || string(hello) != "AIRSIMPCM1\n" {
		return
	}
	if !b.busy.CompareAndSwap(false, true) {
		return
	}
	defer b.busy.Store(false)
	// No probing or unauthenticated LAN connection can create a DSP scene.
	if e = b.serve(ctx, c, r, false); e != nil && !errors.Is(e, context.Canceled) && !errors.Is(e, io.EOF) && !errors.Is(e, net.ErrClosed) {
		b.mu.Lock()
		b.lastError = e.Error()
		b.mu.Unlock()
	}
}
func run(ctx context.Context, name string, args ...string) error {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}
func (b *Bridge) serve(parent context.Context, c net.Conn, r io.Reader, selfTest bool) error {
	downGain, upGain := b.streamGains()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := CheckTools(); err != nil {
		return err
	}
	devices, err := Discover()
	if err != nil {
		return err
	}
	probe, pCancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := exec.CommandContext(probe, "/usr/bin/mu300-at", "-t", "3", "AT+CLCC").Output()
	pCancel()
	if err != nil || !strings.Contains(string(out), "OK") {
		return errors.New("cellular call status unavailable")
	}
	if selfTest && strings.Contains(string(out), "+CLCC:") {
		return errors.New("finish current call before audio self-test")
	}
	if !selfTest && !strings.Contains(string(out), "+CLCC:") {
		return errors.New("no cellular call for PCM session")
	}
	if err = run(ctx, "/usr/bin/mu300-voice", "audio", "start", "dsp"); err != nil {
		return err
	}
	defer func() {
		clean, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_ = run(clean, "/usr/bin/mu300-voice", "audio", "stop")
	}()
	// Hold the AGDSP domain until *both* AP streams have fully closed. The
	// upstream watch service must be stopped by the plugin while we own audio.
	card := strconv.Itoa(devices.Card)
	if err = run(ctx, "amixer", "-q", "-c", card, "cset", "name=agdsp_access_en", "1"); err != nil {
		return err
	}
	// The vendor 5.4 DMA driver cannot safely resubmit the same descriptor after
	// an xrun. Exit and close the stream instead of ALSA's PREPARE/START recovery.
	capture := exec.CommandContext(ctx, "arecord", "--fatal-errors", "-q", "-D", devices.Capture, "-t", "raw", "-f", "S16_LE", "-r", "48000", "-c", "2", "--period-size=960", "--buffer-size=5760")
	play := exec.CommandContext(ctx, "aplay", "--fatal-errors", "-q", "-D", devices.Playback, "-t", "raw", "-f", "S16_LE", "-r", "16000", "-c", "1", "--period-size=320", "--buffer-size=1920")
	captured, err := capture.StdoutPipe()
	if err != nil {
		return err
	}
	played, err := play.StdinPipe()
	if err != nil {
		return err
	}
	if err = play.Start(); err != nil {
		return err
	}
	defer func() { cancel(); played.Close(); _ = play.Wait() }()
	if err = capture.Start(); err != nil {
		return err
	}
	defer func() { cancel(); _ = capture.Wait() }()
	// A disconnected socket unblocks all reader/writer goroutines before return.
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err = io.WriteString(c, "AIRSIMREADY"); err != nil {
		return err
	}
	c.SetDeadline(time.Time{})
	b.mu.Lock()
	b.lastError = ""
	b.mu.Unlock()
	failures := make(chan error, 3)
	frames := &uplinkQueue{}
	go func() {
		for {
			frame := make([]byte, FrameBytes)
			if _, e := io.ReadFull(r, frame); e != nil {
				failures <- e
				return
			}
			b.Received.Add(1)
			if frames.push(frame) {
				b.Dropped.Add(1)
			}
		}
	}()
	go func() {
		d := NewDownsampler()
		raw := make([]byte, 960*4)
		for {
			if _, e := io.ReadFull(captured, raw); e != nil {
				// EOF from the ALSA process is a hardware-stream failure, unlike
				// a client socket closing normally. Preserve it in status.
				if ctx.Err() != nil {
					failures <- ctx.Err()
				} else {
					failures <- errors.New("ALSA capture stream ended")
				}
				return
			}
			frame := d.Convert(raw)
			b.DownlinkLimited.Add(downGain.apply(frame))
			c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if e := writeFull(c, frame); e != nil {
				failures <- e
				return
			}
			b.TX.Add(1)
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	u := &Upsampler{}
	last := time.Now()
	lastWrite := atomic.Int64{}
	lastWrite.Store(time.Now().UnixNano())
	// A wedged ALSA pipe must not block cancellation or the stall watchdog.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				frame := frames.next()
				if frame != nil {
					b.RX.Add(1)
					last = time.Now()
				} else {
					frame = make([]byte, FrameBytes)
					b.Silence.Add(1)
				}
				if time.Since(last) > 10*time.Second {
					failures <- errors.New("PCM uplink stalled")
					return
				}
				playback := u.Convert(frame)
				b.UplinkLimited.Add(upGain.apply(playback))
				if e := writeFull(played, playback); e != nil {
					failures <- e
					return
				}
				lastWrite.Store(time.Now().UnixNano())
			}
		}
	}()
	watch := time.NewTicker(time.Second)
	defer watch.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-failures:
			return e
		case <-watch.C:
			if time.Since(time.Unix(0, lastWrite.Load())) > 3*time.Second {
				return errors.New("ALSA playback stalled")
			}
		}
	}
}

// SelfTest exercises the real DSP streams without placing or answering a call.
// Only the explicit CLI exposes this operation; no LuCI/public HTTP route does.
func (b *Bridge) SelfTest(ctx context.Context, duration time.Duration) error {
	if duration < time.Second || duration > 30*time.Second {
		return errors.New("self-test duration must be 1..30 seconds")
	}
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { defer server.Close(); finished <- b.serve(ctx, server, server, true) }()
	client.SetReadDeadline(time.Now().Add(20 * time.Second))
	ack := make([]byte, len("AIRSIMREADY"))
	if _, e := io.ReadFull(client, ack); e != nil {
		client.Close()
		cancel()
		<-finished
		return e
	}
	client.SetDeadline(time.Time{})
	go func() {
		var frame [FrameBytes]byte
		for {
			if e := writeFull(client, frame[:]); e != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	go func() { io.Copy(io.Discard, client) }()
	select {
	case <-ctx.Done():
		client.Close()
		cancel()
		<-finished
		return ctx.Err()
	case e := <-finished:
		client.Close()
		cancel()
		return fmt.Errorf("audio self-test ended early: %w", e)
	case <-time.After(duration):
	}
	client.Close()
	cancel()
	<-finished
	if b.RX.Load() == 0 || b.TX.Load() == 0 {
		return errors.New("no bidirectional PCM frames")
	}
	return nil
}
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

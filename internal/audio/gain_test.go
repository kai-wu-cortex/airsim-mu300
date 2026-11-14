package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

func gainPCM(samples ...int16) []byte {
	var out []byte
	for _, v := range samples {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	return out
}

func TestGainUnityPreservesAllSamples(t *testing.T) {
	var samples []int16
	for i := -32768; i <= 32767; i++ {
		samples = append(samples, int16(i))
	}
	pcm := gainPCM(samples...)
	want := bytes.Clone(pcm)
	if n := newPCMGain(0, 8000).apply(pcm); n != 0 || !bytes.Equal(pcm, want) {
		t.Fatal("default gain changed audio")
	}
}

func TestGainQuietSpeechAndSilence(t *testing.T) {
	for _, db := range []int{3, 6, 12} {
		pcm := gainPCM(-3000, -1000, 0, 1000, 3000)
		if n := newPCMGain(db, 8000).apply(pcm); n != 0 {
			t.Fatal("quiet audio hit limiter")
		}
		for i, x := range []int16{-3000, -1000, 0, 1000, 3000} {
			want := int16(math.Round(float64(x) * math.Pow(10, float64(db)/20)))
			if got := int16(binary.LittleEndian.Uint16(pcm[i*2:])); got != want {
				t.Fatalf("%d dB sample %d: got %d want %d", db, x, got, want)
			}
		}
	}
}

func TestGainLimiterContinuityAndRecovery(t *testing.T) {
	for _, rate := range []int{8000, 16000} {
		var samples []int16
		for i := 0; i < rate/10; i++ {
			samples = append(samples, 32767, -32768)
		}
		for i := 0; i < rate; i++ {
			samples = append(samples, 1000)
		}
		whole := gainPCM(samples...)
		split := bytes.Clone(whole)
		one := newPCMGain(12, rate)
		n1 := one.apply(whole)
		stream := newPCMGain(12, rate)
		var n2 uint64
		for i := 0; i < len(split); i += 320 {
			n2 += stream.apply(split[i:min(i+320, len(split))])
		}
		if n1 == 0 || n1 != n2 || !bytes.Equal(whole, split) {
			t.Fatal("limiter reset across frames")
		}
		for i, x := range samples {
			y := int16(binary.LittleEndian.Uint16(whole[i*2:]))
			if math.Abs(float64(y)) > gainCeiling || (x > 0 && y < 0) || (x < 0 && y > 0) {
				t.Fatal("overflow or wrong polarity")
			}
		}
		last := int16(binary.LittleEndian.Uint16(whole[len(whole)-2:]))
		if last != 3981 {
			t.Fatalf("gain did not recover after loud passage: %d", last)
		}
		quiet := make([]byte, 320)
		stream.apply(quiet)
		if !bytes.Equal(quiet, make([]byte, 320)) {
			t.Fatal("limiter generated sound during silence")
		}
	}
}

func TestBridgeGainDirectionsAndValidation(t *testing.T) {
	b := &Bridge{}
	for _, pair := range [][2]int{{-1, 0}, {0, -1}, {13, 0}, {0, 13}} {
		if b.ConfigureGain(pair[0], pair[1]) == nil {
			t.Fatal("invalid gain accepted")
		}
	}
	if err := b.ConfigureGain(6, 0); err != nil {
		t.Fatal(err)
	}
	down, up := b.streamGains()
	d, u := gainPCM(1000), gainPCM(1000)
	down.apply(d)
	up.apply(u)
	if int16(binary.LittleEndian.Uint16(d)) != 1995 || int16(binary.LittleEndian.Uint16(u)) != 1000 {
		t.Fatal("direction gains mixed")
	}
	if err := b.ConfigureGain(0, 6); err != nil {
		t.Fatal(err)
	}
	// Existing streams retain their own gain and limiter history.
	d = gainPCM(1000)
	down.apply(d)
	if int16(binary.LittleEndian.Uint16(d)) != 1995 {
		t.Fatal("running stream gain changed")
	}
	newDown, newUp := b.streamGains()
	d, u = gainPCM(1000), gainPCM(1000)
	newDown.apply(d)
	newUp.apply(u)
	if int16(binary.LittleEndian.Uint16(d)) != 1000 || int16(binary.LittleEndian.Uint16(u)) != 1995 {
		t.Fatal("next stream did not get configured gains")
	}
	status := b.Status()
	if status["downlink_gain_db"] != 0 || status["uplink_gain_db"] != 6 {
		t.Fatal("gain missing from diagnostics")
	}
}

func TestEarlyMediaGainMatchesDuplexDownlink(t *testing.T) {
	for _, db := range []int{0, 6, 12} {
		b := &Bridge{}
		if err := b.ConfigureGain(db, 12); err != nil {
			t.Fatal(err)
		}
		// Synthetic stereo source only; no device, network, recording or call.
		var raw []byte
		for i := 0; i < 960*3; i++ {
			x := int16(12000 * math.Sin(2*math.Pi*500*float64(i)/48000))
			raw = append(raw, gainPCM(x, 0)...)
		}
		source := io.NopCloser(bytes.NewReader(raw))
		stream, err := b.openEarlyMedia(context.Background(), func(context.Context) (io.ReadCloser, func(), error) {
			return source, func() {}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, FrameBytes*3)
		_, err = io.ReadFull(stream, got)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := NewDownsampler().Convert(raw)
		limited := newPCMGain(db, 8000).apply(want)
		if !bytes.Equal(got, want) || b.DownlinkLimited.Load() != limited || b.UplinkLimited.Load() != 0 {
			t.Fatal("early media differs from duplex downlink or applied uplink gain")
		}
	}
}

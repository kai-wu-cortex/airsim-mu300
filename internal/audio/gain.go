package audio

import (
	"encoding/binary"
	"errors"
	"math"
)

// ConfigureGain is called before starting the bridge. Each stream takes a
// snapshot, so a gain change cannot cause a step halfway through a call.
func (b *Bridge) ConfigureGain(downlinkDB, uplinkDB int) error {
	if downlinkDB < 0 || downlinkDB > 12 || uplinkDB < 0 || uplinkDB > 12 {
		return errors.New("PCM gain must be 0..12 dB in each direction")
	}
	b.mu.Lock()
	b.downlinkDB, b.uplinkDB = downlinkDB, uplinkDB
	b.mu.Unlock()
	return nil
}

func (b *Bridge) streamGains() (*pcmGain, *pcmGain) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return newPCMGain(b.downlinkDB, 8000), newPCMGain(b.uplinkDB, 16000)
}

// pcmGain belongs to one stream goroutine. The limiter has immediate attack
// and a 50 ms exponential release, retaining state across frame boundaries.
// It adds no buffering, rescales neither sample rate nor frame size, and
// leaves 0 dB byte-for-byte untouched (including existing full-scale samples).
type pcmGain struct {
	factor, reduction, release float64
}

const gainCeiling = 30000.0

func newPCMGain(db, rate int) *pcmGain {
	return &pcmGain{factor: math.Pow(10, float64(db)/20), reduction: 1,
		release: math.Exp(-1 / (0.050 * float64(rate)))}
}

// apply operates only on complete S16LE samples in an owned frame. The caller
// handles TCP fragments with ReadFull before reaching this boundary. The
// return value counts samples attenuated by the limiter, not audio content.
func (g *pcmGain) apply(frame []byte) uint64 {
	if g.factor == 1 {
		return 0
	}
	var limited uint64
	for i := 0; i+1 < len(frame); i += 2 {
		x := float64(int16(binary.LittleEndian.Uint16(frame[i:]))) * g.factor
		g.reduction = 1 - (1-g.reduction)*g.release
		if peak := math.Abs(x); peak*g.reduction > gainCeiling {
			g.reduction = gainCeiling / peak
		}
		if x != 0 && g.reduction < 0.999999 {
			limited++
		}
		y := math.Round(x * g.reduction)
		// Defensive saturation prevents signed integer wrap even at full scale.
		y = math.Max(-gainCeiling, math.Min(gainCeiling, y))
		binary.LittleEndian.PutUint16(frame[i:], uint16(int16(y)))
	}
	return limited
}

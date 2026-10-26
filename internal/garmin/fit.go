package garmin

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Port of the weight-scale half of python-garminconnect's fit.py (FitEncoderWeight), byte for byte.

// BodyComposition is the weight scale message of add_body_composition. Weight is in kg; nil optionals are written as invalid.
type BodyComposition struct {
	Weight                                                                                   float64
	PercentFat, PercentHydration, VisceralFatMass, BoneMass, MuscleMass, BasalMet, ActiveMet *float64
	PhysiqueRating, MetabolicAge, VisceralFatRating, BMI                                     *float64
}

// fitBase is one FIT base type: its definition code, size, invalid sentinel and Python struct format char.
type fitBase struct {
	field, size byte
	invalid     uint32
	format      byte // 'B', 'H' or 'I'
}

var (
	fitEnum    = fitBase{0x00, 1, 0xFF, 'B'}
	fitUint8   = fitBase{0x02, 1, 0xFF, 'B'}
	fitUint16  = fitBase{0x84, 2, 0xFFFF, 'H'}
	fitUint32  = fitBase{0x86, 4, 0xFFFFFFFF, 'I'}
	fitUint32z = fitBase{0x8C, 4, 0, 'I'}
)

// fitField is one (field number, base type, value, scale) entry; a nil value is the invalid sentinel, scale 0 means none.
type fitField struct {
	num   byte
	base  fitBase
	value *float64
	scale float64
}

type fitMessage struct {
	lmsg   byte
	global uint16
	fields []fitField
}

// pack is FitBaseType.pack: int() truncation, then struct.pack, which rejects out-of-range numbers.
func (f fitField) pack() ([]byte, error) {
	var n uint32
	if f.value == nil {
		n = f.base.invalid
	} else {
		v := *f.value
		if f.scale != 0 {
			v *= f.scale
		}
		max := map[byte]float64{'B': 255, 'H': 65535, 'I': 4294967295}[f.base.format]
		if t := float64(int64(v)); t < 0 || t > max {
			return nil, fmt.Errorf("'%c' format requires 0 <= number <= %.0f", f.base.format, max)
		}
		n = uint32(int64(v))
	}
	out := make([]byte, f.base.size)
	switch f.base.size {
	case 1:
		out[0] = byte(n)
	case 2:
		binary.LittleEndian.PutUint16(out, uint16(n))
	default:
		binary.LittleEndian.PutUint32(out, n)
	}
	return out, nil
}

// fitTimestamp is FitEncoder.timestamp: the wall clock of t read in the server's zone (time.mktime), in the FIT epoch.
func fitTimestamp(t time.Time) *float64 {
	v := float64(time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local).Unix() - 631065600)
	return &v
}

func fitCRC(b []byte) uint16 {
	table := [16]uint16{0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401, 0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400}
	var crc uint16
	for _, v := range b {
		crc = (crc>>4)&0x0FFF ^ table[crc&0xF] ^ table[v&0xF]
		crc = (crc>>4)&0x0FFF ^ table[crc&0xF] ^ table[v>>4]
	}
	return crc
}

// encodeWeightFIT builds the file add_body_composition uploads: file_id (created), file_creator, device_info and weight_scale, all at.
func encodeWeightFIT(at, created time.Time, c BodyComposition) ([]byte, error) {
	buf := make([]byte, 12) // header, rewritten by finish
	fp := func(v *float64) *float64 { return v }
	nine := 9.0
	msgs := []fitMessage{
		{0, 0, []fitField{{3, fitUint32z, nil, 0}, {4, fitUint32, fitTimestamp(created), 0}, {1, fitUint16, nil, 0},
			{2, fitUint16, nil, 0}, {5, fitUint16, nil, 0}, {0, fitEnum, &nine, 0}}},
		{1, 49, []fitField{{0, fitUint16, nil, 0}, {1, fitUint8, nil, 0}}},
		{2, 23, []fitField{{253, fitUint32, fitTimestamp(at), 1}, {3, fitUint32z, nil, 1}, {7, fitUint32, nil, 1}, {8, fitUint32, nil, 0},
			{2, fitUint16, nil, 1}, {4, fitUint16, nil, 1}, {5, fitUint16, nil, 100}, {10, fitUint16, nil, 256}, {0, fitUint8, nil, 1},
			{1, fitUint8, nil, 1}, {6, fitUint8, nil, 1}, {11, fitUint8, nil, 0}}},
		{3, 30, []fitField{{253, fitUint32, fitTimestamp(at), 1}, {0, fitUint16, &c.Weight, 100}, {1, fitUint16, fp(c.PercentFat), 100},
			{2, fitUint16, fp(c.PercentHydration), 100}, {3, fitUint16, fp(c.VisceralFatMass), 100}, {4, fitUint16, fp(c.BoneMass), 100},
			{5, fitUint16, fp(c.MuscleMass), 100}, {7, fitUint16, fp(c.BasalMet), 4}, {9, fitUint16, fp(c.ActiveMet), 4},
			{8, fitUint8, fp(c.PhysiqueRating), 1}, {10, fitUint8, fp(c.MetabolicAge), 1}, {11, fitUint8, fp(c.VisceralFatRating), 1},
			{13, fitUint16, fp(c.BMI), 10}}},
	}
	for _, m := range msgs {
		values := []byte{m.lmsg}
		buf = append(buf, 0x40|m.lmsg, 0, 0)
		buf = binary.LittleEndian.AppendUint16(buf, m.global)
		buf = append(buf, byte(len(m.fields)))
		for _, f := range m.fields {
			buf = append(buf, f.num, f.base.size, f.base.field)
			b, err := f.pack()
			if err != nil {
				return nil, err
			}
			values = append(values, b...)
		}
		buf = append(buf, values...)
	}
	copy(buf, []byte{12, 16, 108, 0})
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(buf)-12))
	copy(buf[8:], ".FIT")
	return binary.LittleEndian.AppendUint16(buf, fitCRC(buf)), nil
}

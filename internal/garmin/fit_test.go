package garmin

import (
	"encoding/hex"
	"testing"
	"time"
)

// Golden files produced by python-garminconnect 0.3.2's FitEncoderWeight with TZ=UTC and file_id created 2024-01-15T12:00:00.
const (
	goldenWeightOnly = "0c106c00c60000002e46495440000000000603048c040486010284020284050284000100000000000040d20740ffffffffffff0941000031000200028401010201ffffff42000017000cfd048603048c0704860804860202840402840502840a02840001020101020601020b0102028029074000000000ffffffffffffffffffffffffffffffffffffffff4300001e000dfd04860002840102840202840302840402840502840702840902840801020a01020b01020d02840380290740581bdc05ffffffffffffffffffffffffffffffffff1682"
	goldenAllFields  = "0c106c00c60000002e46495440000000000603048c040486010284020284050284000100000000000040d20740ffffffffffff0941000031000200028401010201ffffff42000017000cfd048603048c0704860804860202840402840502840a02840001020101020601020b010202fd92074000000000ffffffffffffffffffffffffffffffffffffffff4300001e000dfd04860002840102840202840302840402840502840702840902840801020a01020b01020d028403fd920740ce07ca0886157b004001b20c0019800c052107e000f8ea"
)

func f64(v float64) *float64 { return &v }

func TestEncodeWeightFITMatchesPython(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	defer func() { time.Local = old }()
	created := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		c    BodyComposition
		want string
	}{
		{"weight and fat", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), BodyComposition{Weight: 70, PercentFat: f64(15)}, goldenWeightOnly},
		{"all fields", time.Date(2024, 1, 15, 7, 30, 5, 0, time.UTC), BodyComposition{Weight: 19.99, PercentFat: f64(22.5), PercentHydration: f64(55.1),
			VisceralFatMass: f64(1.23), BoneMass: f64(3.2), MuscleMass: f64(32.5), BasalMet: f64(1600), ActiveMet: f64(800), PhysiqueRating: f64(5),
			MetabolicAge: f64(33), VisceralFatRating: f64(7), BMI: f64(22.46)}, goldenAllFields},
	}
	for _, c := range cases {
		got, err := encodeWeightFIT(c.at, created, c.c)
		if err != nil || hex.EncodeToString(got) != c.want {
			t.Errorf("%s:\n got %x\nwant %s (err %v)", c.name, got, c.want, err)
		}
	}
}

package tests

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"wardrobe/internal/store"
)

// ing042RowCount counts weather_locations rows via an independent connection.
func ing042RowCount(t *testing.T, path string) int64 {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	var n int64
	if err := db.Table("weather_locations").Count(&n).Error; err != nil {
		t.Fatalf("count weather_locations: %v", err)
	}
	return n
}

func ing042Open(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := newStorePath(t)
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open() = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func ing042Read(t *testing.T, s *store.Store) store.WeatherLocation {
	t.Helper()
	got, err := s.WeatherLocation()
	if err != nil {
		t.Fatalf("WeatherLocation() = %v, want nil", err)
	}
	return got
}

func ing042Same(t *testing.T, got, want store.WeatherLocation) {
	t.Helper()
	if got.Name != want.Name || got.Country != want.Country ||
		got.Latitude != want.Latitude || got.Longitude != want.Longitude {
		t.Errorf("location = %+v, want %+v", got, want)
	}
}

// AC1: fresh DB returns the Tehran default and the read writes no row.
func TestING042_AC1_DefaultTehranNoWrite(t *testing.T) {
	s, path := ing042Open(t)
	ing042Same(t, ing042Read(t, s), store.WeatherLocation{Name: "Tehran", Country: "Iran", Latitude: 35.69439, Longitude: 51.42151})
	if n := ing042RowCount(t, path); n != 0 {
		t.Errorf("rows after read = %d, want 0", n)
	}
}

// AC2+AC3+AC4: save round-trips; second save replaces; one row; empty country ok.
func TestING042_AC2_AC3_AC4_SaveReplaceSingleRow(t *testing.T) {
	s, path := ing042Open(t)
	first := store.WeatherLocation{Name: "Paris", Country: "France", Latitude: 48.85341, Longitude: 2.3488}
	if err := s.SaveWeatherLocation(first); err != nil {
		t.Fatalf("Save(first) = %v", err)
	}
	ing042Same(t, ing042Read(t, s), first)

	second := store.WeatherLocation{Name: "Null Island", Latitude: 0, Longitude: 0} // empty country, explicit 0s
	if err := s.SaveWeatherLocation(second); err != nil {
		t.Fatalf("Save(second) = %v", err)
	}
	ing042Same(t, ing042Read(t, s), second)
	if n := ing042RowCount(t, path); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

// AC5: invalid input names the field and leaves the stored value (or default) unchanged.
func TestING042_AC5_ValidationNamesFieldNoWrite(t *testing.T) {
	cases := []struct {
		field string
		loc   store.WeatherLocation
	}{
		{"name", store.WeatherLocation{Name: "", Latitude: 1, Longitude: 1}},
		{"name", store.WeatherLocation{Name: " \t\n", Latitude: 1, Longitude: 1}},
		{"latitude", store.WeatherLocation{Name: "X", Latitude: 90.0001, Longitude: 1}},
		{"latitude", store.WeatherLocation{Name: "X", Latitude: -91, Longitude: 1}},
		{"latitude", store.WeatherLocation{Name: "X", Latitude: math.NaN(), Longitude: 1}},
		{"longitude", store.WeatherLocation{Name: "X", Latitude: 1, Longitude: 180.5}},
		{"longitude", store.WeatherLocation{Name: "X", Latitude: 1, Longitude: -181}},
	}
	for _, withSaved := range []bool{false, true} {
		s, path := ing042Open(t)
		want := store.DefaultWeatherLocation
		if withSaved {
			want = store.WeatherLocation{Name: "Paris", Country: "France", Latitude: 48.85341, Longitude: 2.3488}
			if err := s.SaveWeatherLocation(want); err != nil {
				t.Fatalf("Save(valid) = %v", err)
			}
		}
		for _, c := range cases {
			err := s.SaveWeatherLocation(c.loc)
			if !errors.Is(err, store.ErrInvalidLocation) || !strings.Contains(err.Error(), c.field) {
				t.Errorf("Save(%+v) = %v, want ErrInvalidLocation naming %q", c.loc, err, c.field)
			}
			ing042Same(t, ing042Read(t, s), want)
		}
		if n, w := ing042RowCount(t, path), map[bool]int64{false: 0, true: 1}[withSaved]; n != w {
			t.Errorf("withSaved=%v rows = %d, want %d", withSaved, n, w)
		}
	}
}

// AC6: boundary values are accepted.
func TestING042_AC6_BoundariesAccepted(t *testing.T) {
	s, _ := ing042Open(t)
	for _, loc := range []store.WeatherLocation{
		{Name: "S", Latitude: -90, Longitude: -180},
		{Name: "N", Latitude: 90, Longitude: 180},
	} {
		if err := s.SaveWeatherLocation(loc); err != nil {
			t.Errorf("Save(%+v) = %v, want nil", loc, err)
		}
		ing042Same(t, ing042Read(t, s), loc)
	}
}

// AC7: Open on a Phase 1 DB (items table only) adds the table, items untouched.
func TestING042_AC7_MigratePhase1DB(t *testing.T) {
	path := newStorePath(t)
	old, err := store.Open(path) // creates data dir
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	item := sampleItem("0f8c2b1e-0420-4a01-8420-000000000001")
	if err := old.Insert(item); err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	old.Close()

	// Reduce to a Phase 1 schema: drop the weather table.
	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if err := raw.Exec("DROP TABLE weather_locations").Error; err != nil {
		t.Fatalf("drop: %v", err)
	}
	if sqlDB, err := raw.DB(); err == nil {
		sqlDB.Close()
	}
	if _, ok := rawSchemaColumns(t, path)["weather_locations"]; ok {
		t.Fatal("weather_locations still present before re-open")
	}

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("re-Open() = %v", err)
	}
	defer s.Close()
	if _, ok := rawSchemaColumns(t, path)["weather_locations"]; !ok {
		t.Error("weather_locations not added by migration")
	}
	items, err := s.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("List() = %d items, %v; want 1, nil", len(items), err)
	}
	assertItemEqual(t, items[0], item)
	ing042Same(t, ing042Read(t, s), store.DefaultWeatherLocation)
}

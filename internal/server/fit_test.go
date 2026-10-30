package server

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFitMessageNamesMatchFitparse(t *testing.T) {
	b, err := os.ReadFile("testdata/Activity.fit")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := decodeFIT(b)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, m := range messages {
		counts[m.Name]++
	}
	for _, name := range []string{"file_id", "record", "session", "lap", "event", "device_info"} {
		if counts[name] == 0 {
			t.Errorf("missing %s", name)
		}
	}
}
func TestFitMessagePagination(t *testing.T) {
	messages := []fitMessage{{Name: "file_id"}, {Name: "record"}, {Name: "record"}, {Name: "lap"}}
	page := fitPage(messages, 100, map[string]bool{"record": true}, true, 1, 1)
	p := page["pagination"].(map[string]any)
	rows := page["messages"].([]any)
	if p["total_eligible_count"] != 2 || p["returned_count"] != 1 || rows[0].(map[string]any)["message_index"] != 2 {
		t.Fatal(page)
	}
}
func TestExtractFITZip(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("a/activity.fit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("fit")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractFIT(buf.Bytes())
	if err != nil || string(got) != "fit" {
		t.Fatal(string(got), err)
	}
}
func TestSaveDownloadedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.fit")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveDownloadedFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatal(string(got), err)
	}
}

func TestFITContextualExerciseNames(t *testing.T) {
	message := fitMessage{Name: "set", Fields: []fitField{{Name: "category", Value: []any{"deadlift", "squat"}}, {Name: "category_subtype", Value: []any{float64(0), float64(2)}}}}
	applyFITContextualEnums(&message)
	names, ok := message.Fields[1].ValueNames.([]any)
	if !ok || len(names) != 2 || names[0] != "barbell_deadlift" || names[1] != "back_squats" {
		t.Fatal(message.Fields)
	}
}

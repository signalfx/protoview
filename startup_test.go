package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/signalfx/protoview/internal/input"
)

func TestStartupLoadingFlow(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "event.pb")
	schemaPath := filepath.Join(dir, "event.proto")
	payload := protowire.AppendString(
		protowire.AppendTag(nil, 1, protowire.BytesType),
		"stars",
	)
	if err := os.WriteFile(payloadPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schemaPath, []byte(`
		syntax = "proto3";
		package test;
		message Event { string name = 1; }
	`), 0600); err != nil {
		t.Fatal(err)
	}

	model := newStartupModel(payloadPath, schemaPath, "")
	if command := model.Init(); command == nil {
		t.Fatal("Init() returned no command")
	}
	if view := model.View(); !strings.Contains(view, "Reading payload") {
		t.Fatalf("initial view = %q", view)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(startupModel)
	loaded := model.loadPayload()().(payloadLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	updated, command := model.Update(loaded)
	model = updated.(startupModel)
	if model.progressLabel != "Compiling schema" || command == nil {
		t.Fatalf("after payload: progress=%q command=%v", model.progressLabel, command)
	}

	compiled := command().(schemaLoadedMsg)
	if compiled.err != nil {
		t.Fatal(compiled.err)
	}
	updated, command = model.Update(compiled)
	model = updated.(startupModel)
	if model.progressLabel != "Detecting message type" || command == nil {
		t.Fatalf("after schema: progress=%q command=%v", model.progressLabel, command)
	}

	decoded := command().(decodedMsg)
	if decoded.err != nil {
		t.Fatal(decoded.err)
	}
	updated, command = model.Update(decoded)
	model = updated.(startupModel)
	if model.ready == nil || model.progress != 1 || command == nil {
		t.Fatalf("after decode: ready=%v progress=%v command=%v", model.ready, model.progress, command)
	}
	ready, _ := model.Update(startupReadyMsg{})
	if _, ok := ready.(startupModel); ok {
		t.Fatalf("startupReadyMsg did not transition to the viewer: %T", ready)
	}
}

func TestStartupUpdateErrorsAndProgress(t *testing.T) {
	model := newStartupModel("payload", "schema", "")

	updated, command := model.Update(loadingProgressMsg{value: .1, label: "Reading"})
	model = updated.(startupModel)
	if model.progress != .1 || model.progressLabel != "Reading" || command == nil {
		t.Fatalf("progress update = %+v, command=%v", model, command)
	}
	updated, _ = model.Update(loadingProgressMsg{value: .05, label: "Old"})
	model = updated.(startupModel)
	if model.progressLabel != "Reading" {
		t.Fatalf("older progress replaced label: %+v", model)
	}

	if _, command := model.Update(progressStreamClosedMsg{}); command != nil {
		t.Fatal("closed progress stream returned a command")
	}
	if _, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); command == nil {
		t.Fatal("q did not request quit")
	}

	for _, test := range []struct {
		name    string
		message tea.Msg
		want    string
	}{
		{"payload", payloadLoadedMsg{err: errors.New("broken payload")}, "read payload: broken payload"},
		{"schema", schemaLoadedMsg{err: errors.New("broken schema")}, "load schema: broken schema"},
		{"decode", decodedMsg{err: errors.New("broken message")}, "decode protobuf: broken message"},
	} {
		t.Run(test.name, func(t *testing.T) {
			updated, command := model.Update(test.message)
			got := updated.(startupModel)
			if got.err == nil || got.err.Error() != test.want || command == nil {
				t.Fatalf("Update() error=%v command=%v, want %q and quit", got.err, command, test.want)
			}
		})
	}

	model.ready = nil
	if updated, command := model.Update(startupReadyMsg{}); updated.(startupModel).ready != nil || command != nil {
		t.Fatalf("empty ready transition = %#v, %v", updated, command)
	}
}

func TestStartupProgressHelpers(t *testing.T) {
	model := startupModel{}
	model.setProgress(.5, "Halfway")
	model.setProgress(.25, "Older")
	if model.progress != .5 || model.progressLabel != "Halfway" {
		t.Fatalf("setProgress() = %v, %q", model.progress, model.progressLabel)
	}

	updates := make(chan loadingProgressMsg, 1)
	updates <- loadingProgressMsg{value: .75, label: "Working"}
	if got, ok := waitForProgress(updates)().(loadingProgressMsg); !ok || got.value != .75 {
		t.Fatalf("waitForProgress() = %#v", got)
	}
	close(updates)
	if _, ok := waitForProgress(updates)().(progressStreamClosedMsg); !ok {
		t.Fatal("closed progress stream did not return progressStreamClosedMsg")
	}
}

func TestStartupSchemaDecodeFailure(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "event.proto")
	if err := os.WriteFile(schemaPath, []byte(`
		syntax = "proto3";
		package test;
		message Event { string name = 1; }
	`), 0600); err != nil {
		t.Fatal(err)
	}
	model := newStartupModel("payload", schemaPath, "test.Missing")
	model.data = input.Data{Payloads: [][]byte{{}}}

	updated, command := model.Update(payloadLoadedMsg{data: model.data})
	model = updated.(startupModel)
	compiled := command().(schemaLoadedMsg)
	updated, command = model.Update(compiled)
	model = updated.(startupModel)
	decoded := command().(decodedMsg)
	if decoded.err == nil {
		t.Fatal("missing requested message decoded successfully")
	}
	updated, command = model.Update(decoded)
	if updated.(startupModel).err == nil || command == nil {
		t.Fatalf("decode failure transition = %#v, %v", updated, command)
	}
}

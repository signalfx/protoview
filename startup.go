package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/signalfx/protoview/internal/input"
	"github.com/signalfx/protoview/internal/schema"
	"github.com/signalfx/protoview/internal/ui"
)

type payloadLoadedMsg struct {
	data input.Data
	err  error
}

type schemaLoadedMsg struct {
	definitions schema.Result
	err         error
}

type decodedMsg struct {
	typeName string
	messages []protoreflect.Message
	err      error
}

type startupReadyMsg struct{}

type loadingProgressMsg struct {
	value float64
	label string
}

type progressStreamClosedMsg struct{}

type startupModel struct {
	payloadPath, schemaPath, requestedType string
	data                                   input.Data
	definitions                            schema.Result
	width, height                          int
	progress                               float64
	progressLabel                          string
	progressUpdates                        chan loadingProgressMsg
	err                                    error
	ready                                  tea.Model
}

func newStartupModel(payloadPath, schemaPath, requestedType string) startupModel {
	return startupModel{
		payloadPath: payloadPath, schemaPath: schemaPath, requestedType: requestedType,
		progressLabel: "Reading payload", progressUpdates: make(chan loadingProgressMsg, 128),
	}
}

func (m startupModel) Init() tea.Cmd {
	return tea.Batch(m.loadPayload(), waitForProgress(m.progressUpdates))
}

func (m startupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case loadingProgressMsg:
		if message.value > m.progress {
			m.progress = message.value
			m.progressLabel = message.label
		}
		return m, waitForProgress(m.progressUpdates)
	case progressStreamClosedMsg:
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case tea.KeyMsg:
		if message.String() == "q" || message.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case payloadLoadedMsg:
		if message.err != nil {
			m.err = fmt.Errorf("read payload: %w", message.err)
			return m, tea.Quit
		}
		m.data = message.data
		m.setProgress(.22, "Compiling schema")
		return m, func() tea.Msg {
			definitions, err := schema.Load(m.schemaPath)
			return schemaLoadedMsg{definitions: definitions, err: err}
		}
	case schemaLoadedMsg:
		if message.err != nil {
			m.err = fmt.Errorf("load schema: %w", message.err)
			return m, tea.Quit
		}
		m.definitions = message.definitions
		m.setProgress(.25, "Detecting message type")
		return m, func() tea.Msg {
			descriptor, messages, err := m.definitions.DecodeWithProgress(m.data.Payloads, m.requestedType, func(value float64, label string) {
				m.progressUpdates <- loadingProgressMsg{value: .25 + value*.74, label: label}
			})
			close(m.progressUpdates)
			if err != nil {
				return decodedMsg{err: err}
			}
			return decodedMsg{typeName: string(descriptor.FullName()), messages: messages}
		}
	case decodedMsg:
		if message.err != nil {
			m.err = fmt.Errorf("decode protobuf: %w", message.err)
			return m, tea.Quit
		}
		m.setProgress(1, "Ready")
		sizes := make([]int, len(m.data.Payloads))
		for i, payload := range m.data.Payloads {
			sizes[i] = len(payload)
		}
		info := fmt.Sprintf("%s · compression: %s · framing: %s · %d payload(s)", m.payloadPath, m.data.Compression, m.data.Framing, len(m.data.Payloads))
		m.ready = ui.New(message.messages, sizes, message.typeName, m.definitions.CommonPackagePrefix(), info)
		return m, tea.Tick(75*time.Millisecond, func(time.Time) tea.Msg { return startupReadyMsg{} })
	case startupReadyMsg:
		if m.ready != nil {
			ready, command := m.ready.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return ready, command
		}
	}
	return m, nil
}

func (m startupModel) View() string {
	width := max(24, m.width)
	barWidth := min(48, max(12, width-18))
	filled := min(barWidth, int(m.progress*float64(barWidth)))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf("\n  protoview · Protobuf Viewer\n\n  [%s] %3.0f%%\n  %s\n", bar, m.progress*100, m.progressLabel)
}

func (m startupModel) loadPayload() tea.Cmd {
	return func() tea.Msg {
		data, err := input.ReadWithProgress(m.payloadPath, func(value float64) {
			label := "Reading payload"
			if value >= .5 {
				label = "Decompressing payload"
			}
			m.progressUpdates <- loadingProgressMsg{value: value * .2, label: label}
		})
		return payloadLoadedMsg{data: data, err: err}
	}
}

func (m *startupModel) setProgress(value float64, label string) {
	if value > m.progress {
		m.progress = value
		m.progressLabel = label
	}
}

func waitForProgress(updates <-chan loadingProgressMsg) tea.Cmd {
	return func() tea.Msg {
		message, ok := <-updates
		if !ok {
			return progressStreamClosedMsg{}
		}
		return message
	}
}

package schema

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func (r Result) Decode(payloads [][]byte, requested string) (protoreflect.MessageDescriptor, []protoreflect.Message, error) {
	return r.DecodeWithProgress(payloads, requested, nil)
}

func (r Result) DecodeWithProgress(payloads [][]byte, requested string, progress func(float64, string)) (protoreflect.MessageDescriptor, []protoreflect.Message, error) {
	if len(payloads) == 0 {
		return nil, nil, fmt.Errorf("input contains no payload")
	}
	var chosen protoreflect.MessageDescriptor
	if requested != "" {
		for _, md := range r.Messages {
			if string(md.FullName()) == requested {
				chosen = md
				break
			}
		}
		if chosen == nil {
			return nil, nil, fmt.Errorf("message %q not found in schema", requested)
		}
		reportDecode(progress, .1, "Using requested message type")
	} else {
		type candidate struct {
			descriptor protoreflect.MessageDescriptor
			score      int
		}
		var candidates []candidate
		probeCount := min(32, len(payloads))
		for candidateIndex, md := range r.Messages {
			score, valid := 0, true
			for _, payload := range payloads[:probeCount] {
				m := dynamicpb.NewMessage(md)
				if err := proto.Unmarshal(payload, m); err != nil {
					valid = false
					break
				}
				score += messageScore(m.ProtoReflect())
			}
			if valid {
				candidates = append(candidates, candidate{md, score})
			}
			reportDecode(progress, .1*float64(candidateIndex+1)/float64(max(1, len(r.Messages))), "Detecting message type")
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].score != candidates[j].score {
				return candidates[i].score > candidates[j].score
			}
			// Wire-identical messages are inherently ambiguous. Prefer the type
			// in the less-nested package, which is generally the data model root
			// rather than a transport wrapper.
			return strings.Count(string(candidates[i].descriptor.FullName()), ".") <
				strings.Count(string(candidates[j].descriptor.FullName()), ".")
		})
		// Looking only at the first frame is ambiguous because protobuf permits
		// unknown fields. Test the rest in score order and select the first type
		// for which every frame has a valid wire format.
		maxValidated := 0
		for _, candidate := range candidates {
			valid := true
			for payloadIndex, payload := range payloads[probeCount:] {
				if err := proto.Unmarshal(payload, dynamicpb.NewMessage(candidate.descriptor)); err != nil {
					valid = false
					break
				}
				validated := probeCount + payloadIndex + 1
				if validated > maxValidated {
					maxValidated = validated
					if validated%64 == 0 || validated == len(payloads) {
						reportDecode(progress, .1+.5*float64(validated)/float64(len(payloads)), fmt.Sprintf("Validating payload %d/%d", validated, len(payloads)))
					}
				}
			}
			if valid {
				chosen = candidate.descriptor
				break
			}
		}
		if chosen == nil {
			return nil, nil, fmt.Errorf("payload does not decode as any message in the schema")
		}
		reportDecode(progress, .6, "Message type detected")
	}

	messages := make([]protoreflect.Message, len(payloads))
	decodeStart := .1
	if requested == "" {
		decodeStart = .6
	}
	for i, payload := range payloads {
		m := dynamicpb.NewMessage(chosen)
		if err := proto.Unmarshal(payload, m); err != nil {
			return nil, nil, fmt.Errorf("decode payload %d as %s: %w", i+1, chosen.FullName(), err)
		}
		messages[i] = m.ProtoReflect()
		if (i+1)%64 == 0 || i+1 == len(payloads) {
			reportDecode(progress, decodeStart+(1-decodeStart)*float64(i+1)/float64(len(payloads)), fmt.Sprintf("Decoding payload %d/%d", i+1, len(payloads)))
		}
	}
	reportDecode(progress, 1, "Decoded protobuf payloads")
	return chosen, messages, nil
}

func reportDecode(progress func(float64, string), value float64, detail string) {
	if progress != nil {
		progress(min(1, max(0, value)), detail)
	}
}

func messageScore(m protoreflect.Message) int {
	score := -len(m.GetUnknown()) * 2
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		score += 8
		if fd.IsList() {
			list := v.List()
			if fd.Message() != nil {
				score += list.Len() * 2
				for i := 0; i < list.Len(); i++ {
					score += messageScore(list.Get(i).Message())
				}
			}
		} else if fd.IsMap() {
			entries := v.Map()
			score += entries.Len() * 2
			if fd.MapValue().Message() != nil {
				entries.Range(func(_ protoreflect.MapKey, value protoreflect.Value) bool {
					score += messageScore(value.Message())
					return true
				})
			}
		} else if fd.Message() != nil {
			score += messageScore(v.Message())
		}
		return true
	})
	return score
}

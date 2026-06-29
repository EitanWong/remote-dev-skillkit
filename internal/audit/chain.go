package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const ChainSchemaVersion = "rdev.audit-chain.v1"

type Chain struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	EventCount    int          `json:"event_count"`
	RootHash      string       `json:"root_hash"`
	Entries       []ChainEntry `json:"entries"`
}

type ChainEntry struct {
	Sequence     int              `json:"sequence"`
	PreviousHash string           `json:"previous_hash"`
	EventHash    string           `json:"event_hash"`
	ChainHash    string           `json:"chain_hash"`
	Event        model.AuditEvent `json:"event"`
}

func ExportChain(events []model.AuditEvent, generatedAt time.Time) (Chain, error) {
	entries := make([]ChainEntry, 0, len(events))
	previousHash := ""
	for _, event := range events {
		eventHash, err := hashJSON(event)
		if err != nil {
			return Chain{}, err
		}
		chainHash := hashStrings(previousHash, eventHash)
		entries = append(entries, ChainEntry{
			Sequence:     event.Sequence,
			PreviousHash: previousHash,
			EventHash:    eventHash,
			ChainHash:    chainHash,
			Event:        event,
		})
		previousHash = chainHash
	}
	return Chain{
		SchemaVersion: ChainSchemaVersion,
		GeneratedAt:   generatedAt.UTC(),
		EventCount:    len(entries),
		RootHash:      previousHash,
		Entries:       entries,
	}, nil
}

func ExportChainFromJSONL(inputPath, outputPath string, generatedAt time.Time) (Chain, error) {
	events, err := ReadJSONL(inputPath)
	if err != nil {
		return Chain{}, err
	}
	chain, err := ExportChain(events, generatedAt)
	if err != nil {
		return Chain{}, err
	}
	if err := WriteChain(outputPath, chain); err != nil {
		return Chain{}, err
	}
	return chain, nil
}

func VerifyChain(chain Chain) error {
	if chain.SchemaVersion != ChainSchemaVersion {
		return fmt.Errorf("unsupported audit chain schema %q", chain.SchemaVersion)
	}
	if chain.EventCount != len(chain.Entries) {
		return fmt.Errorf("audit chain event count mismatch")
	}
	previousHash := ""
	for index, entry := range chain.Entries {
		if entry.PreviousHash != previousHash {
			return fmt.Errorf("audit chain previous hash mismatch at entry %d", index+1)
		}
		if entry.Sequence != entry.Event.Sequence {
			return fmt.Errorf("audit chain sequence mismatch at entry %d", index+1)
		}
		eventHash, err := hashJSON(entry.Event)
		if err != nil {
			return err
		}
		if entry.EventHash != eventHash {
			return fmt.Errorf("audit chain event hash mismatch at sequence %d", entry.Sequence)
		}
		chainHash := hashStrings(entry.PreviousHash, entry.EventHash)
		if entry.ChainHash != chainHash {
			return fmt.Errorf("audit chain hash mismatch at sequence %d", entry.Sequence)
		}
		previousHash = entry.ChainHash
	}
	if chain.RootHash != previousHash {
		return fmt.Errorf("audit chain root hash mismatch")
	}
	return nil
}

func ReadJSONL(path string) ([]model.AuditEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return DecodeJSONL(file)
}

func DecodeJSONL(reader io.Reader) ([]model.AuditEvent, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var events []model.AuditEvent
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var event model.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode audit jsonl line %d: %w", line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func WriteChain(path string, chain Chain) error {
	content, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func ReadChain(path string) (Chain, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Chain{}, err
	}
	var chain Chain
	if err := json.Unmarshal(content, &chain); err != nil {
		return Chain{}, err
	}
	return chain, nil
}

func hashJSON(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func hashStrings(values ...string) string {
	hasher := sha256.New()
	for _, value := range values {
		_, _ = hasher.Write([]byte(value))
		_, _ = hasher.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

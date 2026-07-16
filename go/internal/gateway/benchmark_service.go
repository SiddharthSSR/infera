package gateway

import (
	"encoding/json"

	"github.com/infera/infera/go/internal/benchmarkspecs"
)

type BenchmarkService interface {
	CatalogPayload() (map[string]json.RawMessage, error)
	ValidateSuite(raw []byte) (map[string]any, error)
	CompareResultIndexes(rawIndexes [][]byte, objective string) (map[string]any, error)
}

type defaultBenchmarkService struct{}

func (defaultBenchmarkService) CatalogPayload() (map[string]json.RawMessage, error) {
	return benchmarkspecs.CatalogPayload()
}

func (defaultBenchmarkService) ValidateSuite(raw []byte) (map[string]any, error) {
	return benchmarkspecs.ValidateSuite(raw)
}

func (defaultBenchmarkService) CompareResultIndexes(rawIndexes [][]byte, objective string) (map[string]any, error) {
	return benchmarkspecs.CompareResultIndexes(rawIndexes, objective)
}

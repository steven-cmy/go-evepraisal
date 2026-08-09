package parsers

import (
	"regexp"
	"strings"
	"testing"

	"github.com/steven-cmy/go-evepraisal"
	"github.com/steven-cmy/go-evepraisal/typedb"
	"github.com/stretchr/testify/assert"
)

type Case struct {
	Description  string
	Input        string
	Expected     ParserResult
	ExpectedRest Input
	RunForAll    bool
}

type CaseGroup struct {
	name  string
	funct func(input Input) (ParserResult, Input)
	cases []Case
}

var ParserTests = []CaseGroup{
	{"assets", ParseAssets, assetListTestCases},
	{"cargo_scans", ParseCargoScan, cargoScanTestCases},
	{"contracts", ParseContract, contractTestCases},
	{"dscan", ParseDScan, dscanTestCases},
	{"listing", ParseListing, listingTestCases},
	{"eft", ParseEFT, eftTestCases},
	{"fitting", ParseFitting, fittingTestCases},
	{"industry", ParseIndustry, industryTestCases},
	{"loot_history", ParseLootHistory, lootHistoryTestCases},
	{"pi", ParsePI, piTestCases},
	{"survey_scanner", ParseSurveyScan, surveyScannerTestCases},
	{"view_contents", ParseViewContents, viewContentsTestCases},
	{"wallet", ParseWallet, walletTestCases},
	{"killmail", ParseKillmail, killmailTestCases},
	{"mining_ledger", ParseMiningLedger, miningLedgerTestCases},
	{"moon_ledger", ParseMoonLedger, moonLedgerTestCases},
	{"compare", ParseCompare, compareTestCases},
}

func TestParsers(rt *testing.T) {
	for _, group := range ParserTests {
		for _, c := range group.cases {
			rt.Run(group.name+":"+c.Description, func(t *testing.T) {
				result, rest := group.funct(StringToInput(c.Input))
				assert.Equal(t, c.Expected, result, "results should be the same")
				assert.Equal(t, c.ExpectedRest, rest, "the rest should be the same")
			})
		}
	}

	for _, group := range ParserTests {
		for _, c := range group.cases {
			if !c.RunForAll {
				continue
			}

			rt.Run("AllParser_"+group.name+":"+c.Description, func(t *testing.T) {
				result, rest := AllParser(StringToInput(c.Input))

				expectedResult := &MultiParserResult{Results: []ParserResult{c.Expected}}
				if c.Expected == nil {
					expectedResult = &MultiParserResult{Results: nil}
				}
				assert.Equal(t, expectedResult, result, "results should be the same")
				assert.Equal(t, c.ExpectedRest, rest, "the rest should be the same")
			})
		}
	}
}

type testDB struct{}

func (testDB) GetType(typeName string) (typedb.EveType, bool) {
	if strings.EqualFold(typeName, "Rampancy Data Dump") {
		return typedb.EveType{Name: "Rampancy Data Dump"}, true
	}
	return typedb.EveType{}, false
}

func (testDB) HasType(typeName string) bool {
	return strings.EqualFold(typeName, "Rampancy Data Dump")
}

func (testDB) GetTypeByID(typeID int64) (typedb.EveType, bool) { return typedb.EveType{}, false }

func (testDB) ListTypes(startingTypeID int64, limit int64) ([]typedb.EveType, error) { return nil, nil }

func (testDB) Search(s string) []typedb.EveType { return nil }

func (testDB) Delete() error { return nil }

func (testDB) Close() error { return nil }

func TestNewContextMultiParserPreservesUnknownTypeLines(t *testing.T) {
	fakeParser := func(input Input) (ParserResult, Input) {
		match, rest := regexParseLines(regexp.MustCompile(`^([\S ]+) ([\d,'\.]+)$`), input)
		if len(match) == 0 {
			return nil, input
		}
		return &Listing{lines: regexMatchedLines(match)}, rest
	}

	p := evepraisal.NewContextMultiParser(testDB{}, []Parser{fakeParser, ParseListing})
	result, rest := p(StringToInput("Rampancy Data Dump 4112"))

	assert.Equal(t, Input{}, rest)
	mp, ok := result.(*MultiParserResult)
	assert.True(t, ok)
	assert.Len(t, mp.Results, 1)
	listing, ok := mp.Results[0].(*Listing)
	assert.True(t, ok)
	assert.Equal(t, "Rampancy Data Dump", listing.Items[0].Name)
	assert.Equal(t, int64(4112), listing.Items[0].Quantity)
}

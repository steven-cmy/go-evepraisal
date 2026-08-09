package evepraisal

import (
	"github.com/steven-cmy/go-evepraisal/parsers"
	"github.com/steven-cmy/go-evepraisal/typedb"
)

// NewContextMultiParser implements a parser that knows about what types exist. This makes it much more powerful
// and prevents accidentally parsing one format as another
func NewContextMultiParser(typeDB typedb.TypeDB, parserList []parsers.Parser) parsers.Parser {
	return parsers.Parser(
		func(input parsers.Input) (parsers.ParserResult, parsers.Input) {
			multiParserResult := &parsers.MultiParserResult{}
			left := input
			for _, parser := range parserList {
				if len(left) == 0 {
					break
				}
				result, nextLeft := parser(left)
				if result != nil && len(result.Lines()) > 0 {
					foundRealType := false
					for _, item := range parserResultToAppraisalItems(result) {
						if typeDB.HasType(item.Name) {
							foundRealType = true
							break
						}
					}

					// If the parser produced matches but none of the parsed items correspond to real types,
					// keep the original input so later parsers can still attempt to parse it.
					if !foundRealType {
						continue
					}
					multiParserResult.Results = append(multiParserResult.Results, result)
					left = nextLeft
				} else {
					left = nextLeft
				}
			}
			return multiParserResult, left
		})
}

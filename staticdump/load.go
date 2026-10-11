package staticdump

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/sethgrid/pester"
	"github.com/steven-cmy/go-evepraisal/typedb"
)

var userAgent = "go-evepraisal (admin@itworks.cc; +https://github.com/steven-cmy/go-evepraisal)"

const latestStaticDumpManifestURL = "https://developers.eveonline.com/static-data/tranquility/latest.jsonl"

func FindLastStaticDumpUrl(client *pester.Client) (string, error) {
	staticDumpURL, _, err := findLastStaticDump(client, latestStaticDumpManifestURL)
	return staticDumpURL, err
}

// FindLastStaticDumpChecksum returns the latest SDE build number as its cache key.
func FindLastStaticDumpChecksum(client *pester.Client) (string, error) {
	_, checksum, err := findLastStaticDump(client, latestStaticDumpManifestURL)
	return checksum, err
}

func findLastStaticDump(client *pester.Client, manifestURL string) (string, string, error) {
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Add("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", fmt.Errorf("unexpected response when fetching latest static dump: %s", resp.Status)
	}

	type manifestEntry struct {
		Key         string `json:"_key"`
		BuildNumber int64  `json:"buildNumber"`
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var entry manifestEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return "", "", fmt.Errorf("decode latest static dump manifest: %w", err)
		}
		if entry.Key != "sde" {
			continue
		}
		if entry.BuildNumber <= 0 {
			return "", "", fmt.Errorf("invalid SDE build number %d", entry.BuildNumber)
		}

		checksum := strconv.FormatInt(entry.BuildNumber, 10)
		staticDumpURL := fmt.Sprintf(
			"https://developers.eveonline.com/static-data/tranquility/eve-online-static-data-%s-yaml.zip",
			checksum,
		)
		return staticDumpURL, checksum, nil
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("read latest static dump manifest: %w", err)
	}
	return "", "", fmt.Errorf("SDE entry not found in latest static dump manifest")
}

func downloadTypes(client *pester.Client, staticDumpURL string, staticDataPath string) error {
	out, err := os.Create(staticDataPath)
	defer func() {
		_ = out.Close()
	}()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", staticDumpURL, nil)
	if err != nil {
		return err
	}

	req.Header.Add("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		rmErr := os.Remove(staticDataPath)
		if rmErr != nil {
			log.Printf("ERROR: Failed to remove type db zip file after failing to fully download it: %s", rmErr)
		}
		return err
	}

	log.Printf("Successfully wrote %d bytes to %s", n, staticDataPath)
	return nil
}

// Type is an eve online type
type Type struct {
	GroupID       int64 `yaml:"groupID"`
	MarketGroupID int64 `yaml:"marketGroupID"`
	Name          struct {
		En string
	}
	Published bool
	Volume    float64
	BasePrice float64
}

// Blueprint is an eve online blueprint
type Blueprint struct {
	BlueprintTypeID int64 `yaml:"blueprintTypeID"`
	Activities      struct {
		Manufacturing struct {
			Materials []struct {
				Quantity int64
				TypeID   int64 `yaml:"typeID"`
			}
			Products []struct {
				Quantity int64
				TypeID   int64 `yaml:"typeID"`
			}
		}
	}
}

func loadtypes(staticDataPath string) ([]typedb.EveType, error) {
	r, err := zip.OpenReader(staticDataPath)
	if err != nil {
		return nil, fmt.Errorf("unzip static data: %w (staticDataPath)", err)
	}
	defer r.Close()

	var allTypes map[int64]Type
	err = loadDataFromZipFile(r, "types.yaml", &allTypes)
	if err != nil {
		return nil, err
	}
	log.Printf("Loaded %d types", len(allTypes))

	var allBlueprints map[int64]Blueprint
	err = loadDataFromZipFile(r, "blueprints.yaml", &allBlueprints)
	if err != nil {
		return nil, err
	}
	log.Printf("Loaded %d blueprints", len(allBlueprints))

	blueprintsByProductType := make(map[int64][]Blueprint)
	for _, blueprint := range allBlueprints {
		for _, product := range blueprint.Activities.Manufacturing.Products {
			blueprints, ok := blueprintsByProductType[product.TypeID]
			if ok {
				blueprintsByProductType[product.TypeID] = append(blueprints, blueprint)
			} else {
				blueprintsByProductType[product.TypeID] = []Blueprint{blueprint}
			}
		}
	}

	types := make([]typedb.EveType, 0)
	for typeID, t := range allTypes {

		// Yep, there is at least one type that isn't named.
		if t.Name.En == "" {
			continue
		}

		eveType := typedb.EveType{
			ID:                typeID,
			GroupID:           t.GroupID,
			MarketGroupID:     t.MarketGroupID,
			Name:              strings.TrimSpace(t.Name.En),
			Volume:            t.Volume,
			BasePrice:         t.BasePrice,
			Aliases:           []string{},
			BlueprintProducts: resolveBlueprintProducts(blueprintsByProductType, typeID),
			Components:        resolveComponents(blueprintsByProductType, typeID),
			BaseComponents:    flattenComponents(resolveBaseComponents(blueprintsByProductType, typeID, 1, 5)),
		}

		name, aliases := computeAliases(typeID, eveType.Name)
		eveType.Name = name
		eveType.Aliases = aliases

		types = append(types, eveType)
	}

	return types, nil
}

func resolveBlueprintProducts(blueprintsByProductType map[int64][]Blueprint, typeID int64) []typedb.Component {
	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Products {
		components = append(components, typedb.Component{Quantity: material.Quantity, TypeID: material.TypeID})
	}
	return components
}

func resolveComponents(blueprintsByProductType map[int64][]Blueprint, typeID int64) []typedb.Component {
	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Materials {
		components = append(components, typedb.Component{Quantity: material.Quantity, TypeID: material.TypeID})
	}
	return components
}

func flattenComponents(components []typedb.Component) []typedb.Component {
	m := make(map[typedb.Component]int64)
	for _, component := range components {
		qty := component.Quantity
		component.Quantity = 0
		m[component] += qty
	}

	s := make([]typedb.Component, 0, len(m))
	for component, qty := range m {
		component.Quantity = qty
		s = append(s, component)
	}
	return s
}

func resolveBaseComponents(blueprintsByProductType map[int64][]Blueprint, typeID int64, multiplier int64, left int) []typedb.Component {
	if left == 0 {
		return nil
	}

	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Materials {
		r := resolveBaseComponents(blueprintsByProductType, material.TypeID, material.Quantity*multiplier, left-1)
		if r == nil {
			components = append(components, typedb.Component{Quantity: material.Quantity * multiplier, TypeID: material.TypeID})
		} else {
			components = append(components, r...)
		}
	}
	return components
}

func parseVolume(s string) (float64, error) {
	// if s == "0E-10" {
	// 	return 0, nil
	// }

	return strconv.ParseFloat(s, 64)
}

func newCSVReader(r io.Reader) *csv.Reader {
	br := bufio.NewReader(r)
	if bom, err := br.Peek(3); err == nil && bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}

	cr := csv.NewReader(br)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	return cr
}

func downloadTypeVolumes(client *pester.Client) (map[int64]float64, error) {
	req, err := http.NewRequest("GET", "https://www.fuzzwork.co.uk/dump/latest/csv/invTypes.csv", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	r := newCSVReader(resp.Body)

	_, err = r.Read()
	if err != nil {
		return nil, err
	}
	volumes := map[int64]float64{}
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		typeID, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			return nil, err
		}

		vol, err := parseVolume(record[5])
		if err != nil {
			return nil, err
		}

		volumes[typeID] = vol
	}
	return volumes, nil
}

func downloadPackagedVolumes(client *pester.Client) (map[int64]float64, error) {
	req, err := http.NewRequest("GET", "https://www.fuzzwork.co.uk/dump/latest/csv/invVolumes.csv", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	r := newCSVReader(resp.Body)
	_, err = r.Read()
	if err != nil {
		return nil, err
	}
	volumes := map[int64]float64{}
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		typeID, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			return nil, err
		}

		vol, err := parseVolume(record[1])
		if err != nil {
			return nil, err
		}

		volumes[typeID] = vol
	}
	return volumes, nil
}

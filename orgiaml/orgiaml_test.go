package orgiaml

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
)

var generate = flag.Bool("generate", false, "(re-)generate fixtures")

func TestUnmarshal(t *testing.T) {
	for _, path := range orgTestFiles() {
		expectedPath := path[:len(path)-len(".org")] + ".json"
		expected := fileString(expectedPath)
		v := map[string]interface{}{}
		err := New().Unmarshal(strings.NewReader(fileString(path)), path, &v)
		if err != nil {
			t.Errorf("%s\n got error: %s", path, err)
			continue
		}
		actual, err := jsonify(v)
		if err != nil {
			t.Errorf("%s\n got error: %s", path, err)
			continue
		}
		if actual != expected {
			t.Errorf("%s:\n%s'", path, diff(actual, expected))
			if *generate {
				log.Println("generating", expectedPath)
				err := ioutil.WriteFile(expectedPath, []byte(actual), 0644)
				if err != nil {
					panic(err)
				}
			}
		} else {
			t.Logf("%s: passed!", path)
		}
	}
}

func jsonify(v interface{}) (string, error) {
	w := strings.Builder{}
	e := json.NewEncoder(&w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	err := e.Encode(v)
	return w.String(), err
}

func orgTestFiles() []string {
	dir := "./testdata"
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(fmt.Sprintf("Could not read directory: %s", err))
	}
	orgFiles := []string{}
	for _, f := range files {
		name := f.Name()
		if filepath.Ext(name) != ".org" {
			continue
		}
		orgFiles = append(orgFiles, filepath.Join(dir, name))
	}
	return orgFiles
}

func fileString(path string) string {
	bs, err := ioutil.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(bs)
}

func diff(actual, expected string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(actual),
		B:        difflib.SplitLines(expected),
		FromFile: "Actual",
		ToFile:   "Expected",
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(diff)
	return text
}

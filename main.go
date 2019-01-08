package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/fsnotify/fsnotify"
	"github.com/niklasfasching/resume/orgiaml"
)

type Resume struct {
	General struct {
		FirstName     string
		LastName      string
		JobName       string
		Address       string
		Email         string
		Phone         string
		URL           string
		Github        string
		Stackoverflow string
		Summary       string
	}

	Experience []struct {
		JobName string
		Company string
		Address string
		Time    string
		Summary string
	}

	Education []struct {
		Certificate string
		Institution string
		Address     string
		Time        string
	}

	Skills          []string
	Recommendations []string
}

var autoReloadScriptTag = `
<script>
  fetch(location.pathname, {method: 'POST'}).then(() => location.reload());
</script>`

var indexTemplate = template.Must(template.New("index").Parse(`
<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8">
    <title>Resume Templates</title>
  </head>
  <body>
  <h1>Resume Templates</h1>
  <ul>
  {{ range . }}
    <li><h2><a href="{{ .Name }}">{{ .Name }}</a></h2></li>
  {{ end }}
  </ul>
  </body>
</html>`))

var errorTemplate = template.Must(template.New("error").Parse(`
<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8">
    <title>ERROR</title>
  </head>
  <body>
  {{ . }}
</html>`))

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		log.Println("USAGE: 'resume RESUME_DATA_FILE' to start a server")
		log.Fatal("USAGE: 'resume RESUME_DATA_FILE TEMPLATE_FILE' to render HTML to stdout")
	}

	if len(os.Args) == 3 {
		html := renderTemplate(os.Args[2], os.Args[1])
		fmt.Fprint(os.Stdout, html)
		return
	}

	resumePath, templatesRoot := os.Args[1], "templates/"

	// a crude version of auto-reload is implemented via long polling:
	// - on load a HTTP POST is fired off. On successful completion of the request the page reloads
	// - the server keeps all POST requests open
	// - when something on disk changes all POST requests are completed (-> the page reloads)
	pending, pendingMutex := []chan bool{}, sync.Mutex{}
	watcher, err := watch([]string{templatesRoot, resumePath}, func(path string) {
		pendingMutex.Lock()
		for _, c := range pending {
			c <- true
		}
		pending = pending[:0]
		pendingMutex.Unlock()
	})
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST":
			pendingMutex.Lock()
			c := make(chan bool)
			pending = append(pending, c)
			pendingMutex.Unlock()
			<-c
		case req.URL.Path == "/":
			w.Write(htmlWithAutoReloadTag(renderIndex(templatesRoot)))
		default:
			templatePath := filepath.Join(templatesRoot, req.URL.Path)
			w.Write(htmlWithAutoReloadTag(renderTemplate(templatePath, resumePath)))
		}
	})
	log.Println("Listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

func watch(paths []string, f func(path string)) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if ok && event.Op&fsnotify.Write == fsnotify.Write {
					f(event.Name)
				}
			case err, ok := <-watcher.Errors:
				if ok {
					log.Println("error:", err)
				}
			}
		}
	}()
	for _, path := range paths {
		err := watcher.Add(path)
		if err != nil {
			return nil, err
		}
	}
	return watcher, nil
}

// render errors as html rather than using http.Error() so we have auto-reload
func renderError(err error) string {
	w := strings.Builder{}
	err = errorTemplate.Execute(&w, err)
	if err != nil {
		return err.Error()
	}
	return w.String()
}

func renderIndex(templatesRoot string) string {
	fs, err := ioutil.ReadDir(templatesRoot)
	if err != nil {
		return renderError(err)
	}
	w := strings.Builder{}
	err = indexTemplate.Execute(&w, fs)
	if err != nil {
		return renderError(err)
	}
	return w.String()
}

func renderTemplate(templatePath, resumePath string) string {
	bs, err := ioutil.ReadFile(resumePath)
	if err != nil {
		return renderError(err)
	}
	resume := Resume{}
	err = orgiaml.New().Unmarshal(bytes.NewReader(bs), resumePath, &resume)
	if err != nil {
		return renderError(err)
	}
	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return renderError(err)
	}
	w := strings.Builder{}
	err = t.Execute(&w, resume)
	if err != nil {
		return renderError(err)
	}
	return w.String()
}

func htmlWithAutoReloadTag(html string) []byte {
	return []byte(strings.Replace(html, "</html>", autoReloadScriptTag+"</html>", 1))
}

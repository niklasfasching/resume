package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
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
		LinkedIn      string
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

	Skills          string
	Recommendations []string
}

var l = log.New(os.Stderr, "", 0)

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

var ChromeExecutableNames = []string{
	"chromium-browser",
	"google-chrome",
}

func main() {
	if len(os.Args) == 1 {
		printUsageAndExit()
	}
	switch cmd := os.Args[1]; cmd {
	case "server":
		if len(os.Args) != 3 {
			printUsageAndExit()
		}
		templatesRoot, resumePath := "templates/", os.Args[2]
		serve(templatesRoot, resumePath)
	case "render":
		if len(os.Args) != 4 {
			printUsageAndExit()
		}
		resumePath, templatePath := os.Args[2], os.Args[3]
		render(templatePath, resumePath)
	default:
		printUsageAndExit()
	}
}

func render(templatePath, resumePath string) {
	html, err := renderTemplate(templatePath, resumePath)
	if err != nil {
		l.Fatalf("Could not render template: %s", err)
	}
	err = ioutil.WriteFile("resume.html", []byte(html), 0644)
	if err != nil {
		l.Fatalf("Could not write resume.html: %s", err)
	}
	chromePath := chromeExecutable()
	if chromePath == "" {
		l.Println("Skip resume.pdf rendering: chrome not found & CHROME_EXECUTABLE env var not set")
		os.Exit(0)
	}
	cmd := exec.Command(chromePath,
		"--headless",
		"--disable-gpu",
		"--virtual-time-budget=10000",
		"--print-to-pdf=resume.pdf",
		"resume.html")
	err = cmd.Run()
	if err != nil {
		l.Fatal("Could not render resume.pdf:", err)
	}
}

func printUsageAndExit() {
	l.Println("USAGE: 'resume server RESUME_ORG_PATH': Start server on :8000 and render resume templates")
	l.Println("USAGE: 'resume render RESUME_ORG_PATH TEMPLATE_PATH': Render resume.html & resume.pdf in current directory")
	os.Exit(0)
}

func serve(templatesRoot, resumePath string) {
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
		l.Fatal(err)
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
			w.Write(renderWithAutoReload(renderIndex(templatesRoot)))
		default:
			templatePath := filepath.Join(templatesRoot, req.URL.Path)
			w.Write(renderWithAutoReload(renderTemplate(templatePath, resumePath)))
		}
	})
	l.Println("Listening on :8000")
	l.Fatal(http.ListenAndServe(":8000", nil))
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
					l.Println("error:", err)
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

func renderIndex(templatesRoot string) (string, error) {
	fs, err := ioutil.ReadDir(templatesRoot)
	if err != nil {
		return "", err
	}
	w := strings.Builder{}
	err = indexTemplate.Execute(&w, fs)
	if err != nil {
		return "", err
	}
	return w.String(), nil
}

func renderTemplate(templatePath, resumePath string) (string, error) {
	bs, err := ioutil.ReadFile(resumePath)
	if err != nil {
		return "", err
	}
	resume := Resume{}
	err = orgiaml.New().Unmarshal(bytes.NewReader(bs), resumePath, &resume)
	if err != nil {
		return "", err
	}
	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", err
	}
	w := strings.Builder{}
	err = t.Execute(&w, resume)
	if err != nil {
		return "", err
	}
	return w.String(), nil
}

func renderWithAutoReload(html string, err error) []byte {
	if err != nil {
		w := strings.Builder{}
		err = errorTemplate.Execute(&w, err)
		if err != nil {
			html = "<!DOCTYPE html><html><body>" + err.Error() + "</body></html>"
		} else {
			html = w.String()
		}
	}
	return []byte(strings.Replace(html, "</html>", autoReloadScriptTag+"</html>", -1))
}

func chromeExecutable() string {
	if path := os.Getenv("CHROME_EXECUTABLE"); path != "" {
		return path
	}
	for _, name := range ChromeExecutableNames {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"code.google.com/p/go-sqlite/go1/sqlite3"
	"github.com/PuerkitoBio/goquery"
)

type Rss struct {
	Date      string     `xml:"channel>lastBuildDate"`
	Subtitles []Subtitle `xml:"channel>item"`
}

type Subtitle struct {
	Show    string `xml:"category"`
	Title   string `xml:"title"`
	Date    string `xml:"pubDate"`
	Content string `xml:"encoded"`
}

var (
	db    *sqlite3.Conn
	dir   string
	shows map[string]int
)

var releases = []string{
	"720p.*-(DIMENSION|KILLERS|IMMERSE|2HD)",
	"720p.*-(ASAP)",
	"-(LOL|KILLERS|ASAP|2HD)",
}

func start() {
	fmt.Print("Starting ASSED\n\n")

	hostname, _ := os.Hostname()

	if hostname == "assed" {
		dir = "/var/lib/assed/"
	}

	initDB()

	os.MkdirAll(dir+"databases", 0755)
	os.MkdirAll(dir+"subtitles", 0755)
	os.MkdirAll(dir+"downloads", 0755)
	os.MkdirAll(dir+"completed", 0755)
	os.MkdirAll(dir+"finalized", 0755)
	fmt.Println("- Directories OK")

	getShows()

	fmt.Print("\n")
}

func initDB() {
	if db != nil {
		return
	}

	var err error

	db, err = sqlite3.Open(dir + "databases/assed.db")
	if err != nil {
		log.Fatalf("Unable to init SQLite: %s", err.Error())
	}

	fmt.Println("- SQLite OK")
}

func getShows() {
	shows = make(map[string]int)

	sql := "SELECT id, name FROM shows"
	for row, err := db.Query(sql); err == nil; err = row.Next() {
		var id int
		var name string

		row.Scan(&id, &name)
		shows[name] = id
	}

	fmt.Printf("- Shows List OK (%d)\n", len(shows))
}

func getURL(url string) []byte {
	res, err := http.Get(url)
	if err != nil {
		log.Fatalf("Unable to retrieve url %s: %s", url, err.Error())
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalf("Unable to read body of %s: %s", url, err.Error())
	}

	return body
}

func getRSS() Rss {
	body := getURL("http://legendafacil.com/feed/")

	var rss Rss
	err := xml.Unmarshal(body, &rss)
	if err != nil {
		log.Fatalf("Unable to unmarshal subtitles feed: %s", err.Error())
	}

	return rss
}

func needDownload(item Subtitle) bool {
	if shows[item.Show] == 0 {
		fmt.Println("not on the list")
		db.Exec("INSERT INTO ignored (name, date) VALUES (?, ?)", item.Title, time.Now().Unix())

		return false
	}

	_, err := db.Query("SELECT id FROM episodes WHERE name = ? LIMIT 1", item.Title)
	if err == nil {
		fmt.Println("already processed")
		return false
	}

	return true
}

func getMagnet(title string) string {
	page, err := goquery.NewDocument(fmt.Sprintf(
		"https://kickass.to/usearch/%s/?field=seeders&sorder=desc", url.QueryEscape(title)))
	if err != nil {
		log.Fatalf("Unable to parse torrents page: %s", err.Error())
	}

	magnet, _ := page.Find("a.imagnet").Attr("href")

	return magnet
}

func getSRT(url string) []byte {
	var srt []byte

	regex := regexp.MustCompile("^https")
	url = regex.ReplaceAllString(url, "http")

	body := getURL(url)

	regex = regexp.MustCompile("^(.*\n)*.*(https?://[^?]+\\?edmc=[0-9]+).*(\n.*)*$")
	if !regex.MatchString(string(body)) {
		return srt
	}

	srtURL := regex.ReplaceAllString(string(body), "$2")
	client := &http.Client{}

	request, err := http.NewRequest("GET", srtURL, nil)
	request.Header.Add("Referer", url)
	request.Header.Add("User-agent",
		`Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10; rv:33.0) Gecko/20100101 Firefox/33.0`)

	res, err := client.Do(request)
	if err != nil {
		log.Fatalf("Unable to retrieve subtitle file: %s", err.Error())
	}
	defer res.Body.Close()

	srt, err = ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalf("Unable to read subtitle file: %s", err.Error())
	}

	return srt
}

func moveCompleted(path string, level int) {
	files, _ := ioutil.ReadDir(path)
	for _, f := range files {
		if f.IsDir() {
			moveCompleted(fmt.Sprintf("%s/%s", path, f.Name()), level+1)
		} else {
			regex := regexp.MustCompile("(?i)^(.+)\\.(mkv|avi|mp4|mpe?g)$")
			if regex.MatchString(f.Name()) {
				extension := regex.ReplaceAllString(f.Name(), "$2")
				filename := regex.ReplaceAllString(f.Name(), "$1")

				regex = regexp.MustCompile("[ .]+")
				filename = regex.ReplaceAllString(filename, ".")

				if subtitle := findSubtitle(filename + ".srt"); subtitle != "" {
					destPath := dir + "finalized"

					err := os.Rename(
						fmt.Sprintf("%s/%s", path, f.Name()),
						fmt.Sprintf("%s/%s.%s", destPath, filename, extension))

					if err == nil {
						os.Rename(subtitle, fmt.Sprintf("%s/%s.%s", destPath, filename, "srt"))

						if level > 0 {
							os.RemoveAll(path)
						}
					}
				}

				return
			}
		}
	}
}

func findSubtitle(filename string) string {
	path := dir + "subtitles"
	files, _ := ioutil.ReadDir(path)

	for _, f := range files {
		if strings.EqualFold(filename, f.Name()) {
			return fmt.Sprintf("%s/%s", path, f.Name())
		}
	}

	return ""
}

func main() {
	start()

	rss := getRSS()
	var count int

	for _, item := range rss.Subtitles {
		fmt.Printf("%s ... ", item.Title)

		if !needDownload(item) {
			continue
		}

		r := bytes.NewReader([]byte(item.Content))
		content, err := goquery.NewDocumentFromReader(r)
		if err != nil {
			log.Fatalf("Unable to parse subtitles feed content: %s", err.Error())
		}

		var oneHit bool

	Releases:
		for _, release := range releases {
			for _, node := range content.Find("table tbody td:first-child a").Nodes {
				link := goquery.NewDocumentFromNode(node)
				name := link.Text()

				regex := regexp.MustCompile("[0-9]+$")
				name = regex.ReplaceAllString(name, "")

				if match, _ := regexp.MatchString("(?i)"+release, name); match {
					oneHit = true
					fmt.Printf("matched release %s ... ", name)

					magnet := getMagnet(name)
					if magnet == "" {
						fmt.Println("magnet not found")
						continue
					}

					href, _ := link.Attr("href")

					srt := getSRT(href)
					if len(srt) == 0 {
						fmt.Println("subtitle download failed")
						continue
					}

					regex = regexp.MustCompile("[ .]+")
					name = regex.ReplaceAllString(name, ".")

					err := ioutil.WriteFile(fmt.Sprintf(dir+"subtitles/%s.srt", name), srt, 0644)
					if err != nil {
						log.Fatalf("Unable to save subtitle file: %s", err.Error())
					}

					db.Exec("UPDATE shows SET last = ? WHERE id = ?", time.Now().Unix(), shows[item.Show])

					db.Exec("INSERT INTO episodes (show, name, magnet, date) VALUES (?, ?, ?, ?)",
						shows[item.Show], item.Title, magnet, time.Now().Unix())

					exec.Command("transmission-remote", "-a", magnet).Run()

					count++
					fmt.Println("OK")

					break Releases
				}
			}
		}

		if !oneHit {
			db.Exec("INSERT INTO mismatch (name, show, date) VALUES (?, ?, ?)",
				item.Title, shows[item.Show], time.Now().Unix())

			fmt.Println("no release matched this episode")
		}
	}

	moveCompleted(dir+"completed", 0)

	fmt.Print("\n")
	fmt.Printf("Finished... Items processed: %d\n", count)
}

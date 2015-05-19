package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"code.google.com/p/mahonia"
	"github.com/PuerkitoBio/goquery"
	"github.com/andlabs/ui"
)

type Subtitles struct {
	ID        string
	Languages string
	Title     string
}

type AjaxData struct {
	Success bool   `json:"success"`
	Url     string `json:"url"`
}

type GuessName struct {
	Name string
}

type Result struct {
	Title     string
	SubTitle  string
	Languages string
}

var (
	window      ui.Window
	name        ui.TextField
	savepath    ui.TextField
	guessTable  ui.Table
	resultTable ui.Table
	msgbar      ui.Label

	unrar      string
	moviepath  string
	guessNames []GuessName
)

func main() {
	flag.StringVar(&unrar, "unrar", "unrar", "")
	flag.Parse()

	if flag.NArg() > 0 {
		moviepath = flag.Arg(0)
	}

	go ui.Do(func() {
		name = ui.NewTextField()
		searchButton := ui.NewButton("search")
		searchButton.OnClicked(func() {
			msgbar.SetText("")
			subs := searchSub(name.Text())
			resultTable.Lock()
			if len(subs) > 0 {
				data := resultTable.Data().(*[]Subtitles)
				*data = subs
			} else {
				msgbar.SetText("未找到匹配的字幕")
			}
			resultTable.Unlock()
		})
		guessTable = ui.NewTable(reflect.TypeOf(GuessName{}))
		guessTable.OnSelected(func() {
			data := guessTable.Data().(*[]GuessName)
			t := *data
			name.SetText(t[guessTable.Selected()].Name)

		})
		left := ui.NewVerticalStack(
			guessTable,
			name,
			searchButton)
		left.SetStretchy(0)
		resultTable = ui.NewTable(reflect.TypeOf(Subtitles{}))
		savepath = ui.NewTextField()
		if len(moviepath) > 0 {
			savepath.SetText(filepath.Dir(moviepath))
		} else {
			savepath.SetText("/tmp")
		}
		download := ui.NewButton("download")
		download.OnClicked(func() {
			msgbar.SetText("")
			if resultTable.Selected() < 0 {
				return
			}
			data := resultTable.Data().(*[]Subtitles)
			s := *data
			item := s[resultTable.Selected()]
			err := downloadSub(item.ID)
			if err != nil {
				msgbar.SetText(err.Error())
			} else {
				msgbar.SetText("下载成功!")
			}
		})
		right := ui.NewVerticalStack(resultTable, savepath, download)
		right.SetStretchy(0)
		main := ui.NewHorizontalStack(left, right)
		main.SetStretchy(0)
		main.SetStretchy(1)

		msgbar = ui.NewLabel("")
		root := ui.NewVerticalStack(main, msgbar)
		root.SetStretchy(0)
		window = ui.NewWindow("gosubhd", 800, 500, root)
		window.SetMargined(true)
		if len(moviepath) > 0 {
			guessTable.Lock()
			d := guessTable.Data().(*[]GuessName)
			*d = guessName(moviepath)
			guessTable.Unlock()
		}
		window.OnClosing(func() bool {
			ui.Stop()
			return true
		})
		window.Show()
	})
	err := ui.Go()
	if err != nil {
		panic(err)
	}
}

func filter(name string, filterStr string) string {
	re, err := regexp.Compile(filterStr)
	if err != nil {
		log.Fatal(err)
	}
	result := string(re.Find([]byte(name)))
	if len(result) > 0 {
		return strings.TrimSpace(strings.Replace(name, result, "", 1))
	}
	return name
}

func guessName(fpath string) []GuessName {
	guessNames := make([]GuessName, 0)
	filename := filepath.Base(fpath)
	ext := filepath.Ext(filename)
	basename := filename[:len(filename)-len(ext)]
	basename = strings.Replace(basename, ".", " ", -1)
	basename = strings.Replace(basename, "-", " ", -1)
	filterStrs := []string{`\d{3,4}x\d{3,4}`, `^飞鸟娱乐[\[(].+[\])]`, `YYeTs人人影视`}
	for _, filterStr := range filterStrs {
		if basename != filter(basename, filterStr) {
			basename = filter(basename, filterStr)
		}
	}
	//按年份分割
	re, err := regexp.Compile(`\d{4}`)
	if err != nil {
		log.Fatal(err)
	}
	result := string(re.Find([]byte(basename)))
	index := strings.Index(basename, result)
	hasYear := false
	if index > 0 {
		hasYear = true
		if basename[index-1] == ' ' && basename[index+4] == ' ' {
			guessNames = append(guessNames, GuessName{Name: basename[:index-1]})
		}
	}
	//按分辨率分割
	if !hasYear {
		for _, ratio := range []string{"1080P", "720P", "360P"} {
			index = strings.Index(strings.ToUpper(basename), ratio)
			if index > 0 {
				guessNames = append(guessNames, GuessName{Name: basename[:index-1]})
				break
			}
		}
	}
	guessNames = append(guessNames, GuessName{Name: basename})
	return guessNames
}

func searchSub(keyword string) []Subtitles {
	doc, err := goquery.NewDocument("http://www.subhd.com/search/" + keyword)
	if err != nil {
		log.Fatal(err)
	}
	subs := make([]Subtitles, 0)
	doc.Find(".col-md-9 .box").Each(func(n int, node *goquery.Selection) {
		var sub Subtitles
		sub.Title = node.Find(".d_title").Text()
		link, _ := node.Find(".d_title a").Attr("href")
		link_split := strings.Split(link, "/")
		sub.ID = link_split[len(link_split)-1]
		languages := make([]string, 0)
		node.Find(".label").Each(func(_ int, label *goquery.Selection) {
			lan := strings.TrimSpace(label.Text())
			if lan != "" && lan != "字幕翻译" {
				languages = append(languages, lan)
			}
		})
		sub.Languages = strings.Join(languages, ",")
		subs = append(subs, sub)
	})
	return subs
}

func downloadSub(id string) error {
	SubTypes := []string{"ass", "srt", "sub", "idx"}
	res, err := http.PostForm("http://subhd.com/ajax/down_ajax", url.Values{"sub_id": {id}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var ajaxdata AjaxData
	_ = json.Unmarshal(body, &ajaxdata)
	ext := filepath.Ext(ajaxdata.Url)
	tempfile, err := ioutil.TempFile("/tmp", "gosubhd_")
	if err != nil {
		return err
	}
	defer os.Remove(tempfile.Name())
	defer tempfile.Close()
	resp1, err := http.Get(ajaxdata.Url)
	if err != nil {
		return err
	}
	defer resp1.Body.Close()
	content, err := ioutil.ReadAll(resp1.Body)
	if err != nil {
		return err
	}
	_, err = tempfile.Write(content)
	if err != nil {
		return err
	}
	switch ext {
	case ".rar":
		_, err = exec.LookPath(unrar)
		if err != nil {
			return errors.New(unrar + " not found")
		}
		tempdir, err := ioutil.TempDir("/tmp", "gosubhd_")
		if err != nil {
			return err
		}
		cmd := exec.Command(unrar, "x", tempfile.Name(), tempdir)
		err = cmd.Run()
		if err != nil {
			return err
		}
		filepath.Walk(tempdir, func(path string, fi os.FileInfo, err error) error {
			if isSubtitles(path, SubTypes) {
				err = os.Rename(path, filepath.Join(savepath.Text(), filepath.Base(path)))
				if err != nil {
					return err
				}
			}
			return nil
		})
		err = os.RemoveAll(tempdir)
		if err != nil {
			return err
		}
	case ".zip":
		zipr, err := zip.OpenReader(tempfile.Name())
		if err != nil {
			return err
		}
		defer zipr.Close()
		for _, f := range zipr.File {
			if !isSubtitles(f.Name, SubTypes) {
				continue
			}
			dec := mahonia.NewDecoder("GBK")
			fpath := filepath.Join(savepath.Text(), filepath.Base(dec.ConvertString(f.Name)))
			if f.FileInfo().Mode().IsRegular() {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				content, err := ioutil.ReadAll(rc)
				if err != nil {
					return err
				}
				err = ioutil.WriteFile(fpath, content, 0666)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isSubtitles(fullpath string, slice []string) bool {
	for _, v := range slice {
		if strings.ToUpper(filepath.Ext(fullpath)) == strings.ToUpper("."+v) {
			return true
		}
	}
	return false
}

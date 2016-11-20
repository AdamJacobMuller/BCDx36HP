package main

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"golang.org/x/image/riff"
)

type Handler struct {
	Base  string
	Cache string
}

func (f *Handler) gi(r *http.Request, t string) (string, string) {
	params := mux.Vars(r)
	wFile := fmt.Sprintf("%s/%s.wav", f.Base, params["path"])
	cacheStr := fmt.Sprintf("%s-%s", params["path"], r.URL.RawQuery)
	cFile := fmt.Sprintf("%s/%x.%s", f.Cache, sha1.Sum([]byte(cacheStr)), t)
	return wFile, cFile
}
func (f *Handler) ServeError(err error, rw http.ResponseWriter) {
	http.Error(rw, fmt.Sprintf("%s", err), 504)
}
func (f *Handler) ServeCached(filename string, rw http.ResponseWriter) bool {
	fh, err := os.Open(filename)
	if os.IsNotExist(err) {
		return false
	}
	io.Copy(rw, fh)
	fh.Close()
	return true
}

func (f *Handler) sendWAV(rw http.ResponseWriter, r *http.Request) {
	wFile, cFile := f.gi(r, "wav")
	log.WithFields(log.Fields{"wavFile": wFile, "cacheFile": cFile, "url": r.URL.Path}).Info("sendWAV")
	if f.ServeCached(wFile, rw) {
		return
	}
}
func (f *Handler) sendPNG(rw http.ResponseWriter, r *http.Request) {
	wFile, cFile := f.gi(r, "png")
	log.WithFields(log.Fields{"wavFile": wFile, "cacheFile": cFile, "url": r.URL.Path}).Info("sendPNG")
	if f.ServeCached(cFile, rw) {
		return
	}
	socks := exec.Command("sox", wFile, "-n", "spectrogram", "-Y", "130", "-o", cFile)
	err := socks.Start()
	if err != nil {
		log.WithFields(log.Fields{"wavFile": wFile, "error": err}).Info("exec.Command failed")
		f.ServeError(err, rw)
		return
	}
	err = socks.Wait()
	if err != nil {
		log.WithFields(log.Fields{"wavFile": wFile, "error": err}).Info("exec.Wait failed")
		f.ServeError(err, rw)
		return
	}
	if f.ServeCached(cFile, rw) {
		return
	}
}
func (f *Handler) sendJSON(rw http.ResponseWriter, r *http.Request) {
	wFile, cFile := f.gi(r, "json")
	log.WithFields(log.Fields{"wavFile": wFile, "cacheFile": cFile, "url": r.URL.Path}).Info("sendJSON")
	if f.ServeCached(cFile, rw) {
		return
	}
	info, err := getUnidenRiffInfo(wFile)
	if err != nil {
		log.WithFields(log.Fields{"wavFile": wFile, "error": err}).Info("getUnidenRiffInfo failed")
		f.ServeError(err, rw)
		return
	}
	jsonData, err := json.Marshal(info)
	if err != nil {
		log.WithFields(log.Fields{"wavFile": wFile, "error": err}).Info("json.Marshal failed")
		f.ServeError(err, rw)
		return
	}
	err = ioutil.WriteFile(cFile, jsonData, 0644)
	if err != nil {
		log.WithFields(log.Fields{"wavFile": wFile, "cacheFile": cFile, "error": err}).Info("ioutil.WriteFile failed")
		f.ServeError(err, rw)
		return
	}
	if f.ServeCached(cFile, rw) {
		return
	}
}
func (f *Handler) sendList(rw http.ResponseWriter, r *http.Request) {
	log.WithFields(log.Fields{"url": r.URL.Path}).Info("sendList")
	params := mux.Vars(r)
	lPath := fmt.Sprintf("%s/%s", f.Base, params["path"])
	files, err := ioutil.ReadDir(lPath)
	if err != nil {
		log.WithFields(log.Fields{"listPath": lPath, "error": err}).Info("ioutil.ReadDir failed")
		f.ServeError(err, rw)
		return
	}
	rw.Write([]byte("<table>"))
	for _, f := range files {
		rw.Write([]byte("<tr>"))
		if f.IsDir() {
			rw.Write([]byte("<td colspan=\"2\">"))
			rw.Write([]byte(fmt.Sprintf("<a href=\"%s/\">%s</a>\n", f.Name(), f.Name())))
			rw.Write([]byte("</td>"))
		} else if strings.HasSuffix(f.Name(), ".wav") {
			rw.Write([]byte("<td>"))
			rw.Write([]byte(fmt.Sprintf("<img src=\"%s.png\" />\n", strings.TrimRight(f.Name(), ".wav"))))
			rw.Write([]byte("</td>"))
			rw.Write([]byte("<td>"))
			rw.Write([]byte(fmt.Sprintf("<audio controls><source src=\"%s\" type=\"audio/wav\">\n", f.Name())))
			rw.Write([]byte("</td>"))
		}
		rw.Write([]byte("</tr>"))
	}
	rw.Write([]byte("</table>"))
}
func (f *Handler) sendIndex(rw http.ResponseWriter, r *http.Request) {
	log.WithFields(log.Fields{"url": r.URL.Path}).Info("sendIndex")
}
func main() {
	h := Handler{}
	h.Base = "/Users/adam/Scripts/apps/BCDx36HP/data"
	h.Cache = "/Users/adam/Scripts/apps/BCDx36HP/cache"
	r := mux.NewRouter()
	r.HandleFunc("/{path:.*}.json", h.sendJSON)
	r.HandleFunc("/{path:.*}.png", h.sendPNG)
	r.HandleFunc("/{path:.*}.wav", h.sendWAV)
	r.HandleFunc("/{path:.*}/", h.sendList)
	r.HandleFunc("/", h.sendIndex)
	s := &http.Server{
		Addr:           ":8080",
		Handler:        r,
		MaxHeaderBytes: 1 << 20,
	}
	log.Fatal(s.ListenAndServe())

	list, err := getUnidenRiffInfo(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	for k, v := range list {
		fmt.Printf("%10s %s\n", k, v)
	}
}

func getUnidenRiffInfo(file string) (map[string]string, error) {
	fh, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	_, r, err := riff.NewReader(fh)
	if err != nil {
		log.Fatal(err)
	}
	chunkID, chunkLen, chunkData, err := r.Next()
	if err != nil {
		return map[string]string{}, err
	}
	if chunkID == riff.LIST {
		_, list, err := riff.NewListReader(chunkLen, chunkData)
		if err != nil {
			return map[string]string{}, err
		}
		infoList := map[string]string{}
		for {
			chunkID, _, chunkData, err := list.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return map[string]string{}, err
			}
			b, err := ioutil.ReadAll(chunkData)
			if err != nil {
				return map[string]string{}, err
			}
			v := strings.TrimRight(string(b), "\x00")
			infoList[fmt.Sprintf("%s", chunkID)] = v
		}
		return infoList, nil
	} else {
		return map[string]string{}, errors.New("chunk is not riff.LIST")
	}
}

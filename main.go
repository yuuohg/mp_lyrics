package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/panjf2000/ants/v2"
)

const lrclibAPI = "https://lrclib.net/api/search?"

const ESC = "\x1b["
const RESET = ESC + "m"
const BOLD = ESC + "1" + "m"
const RED = ESC + "31" + "m"
const GREEN = ESC + "32" + "m"
const YELLOW = ESC + "33" + "m"
const BLUE = ESC + "34" + "m"
const MAGENTA = ESC + "35" + "m"

type Song struct {
	title, artist, album, id string
}

type Query struct {
	q, title, artist, album, message string
}

type SearchResult struct {
	Id, TrackName, AlbumName, Duration, Instrumental, PlainLyrics, SyncedLyrics string
}

type Lyric struct {
	lyrics string
	song   Song
	synced bool
}

func (s *Song) GetPathForSong(dir string) string {
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	fileName := path.Join(dir, replacer.Replace(s.title)) + ".txt"
	return fileName
}

func (ly *Lyric) SaveToFile(dir string) error {
	file, err := os.Create(ly.song.GetPathForSong(dir))
	if err != nil {
		return AddError(err)
	}
	_, err = file.WriteString(ly.lyrics)
	if err != nil {
		return AddError(err)
	}
	return nil
}

func SongFromSlice(sl []string) (Song, error) {
	if len(sl) != 4 {
		return Song{}, AddError(fmt.Errorf("len of slice: %v, expected 4", len(sl)))
	}
	song := Song{sl[0], sl[1], sl[2], sl[3]}
	return song, nil
}

func (s Song) String() string {
	return fmt.Sprintf("\nTitle: %v\nArtist: %v\nAlbum: %v\nId: %v\n", s.title, s.artist, s.album, s.id)
}

func (s *Song) makeQueries() []Query {
	queries := make([]Query, 0, 4)
	titleArtist := fmt.Sprintf("%v %v", s.title, s.artist)
	titleArtistAlbum := fmt.Sprintf("%v %v %v", s.title, s.artist, s.album)
	queries = append(queries, Query{q: titleArtist, message: "Searching using title and artist for '&s'"})
	queries = append(queries, Query{title: s.title, artist: s.artist, album: s.album, message: "Searching in all fields for '&s'"})
	queries = append(queries, Query{q: titleArtistAlbum, message: "Searching using full track info for '&s'"})
	titleHasSep, artistHasSep := strings.Contains(s.title, " - "), strings.Contains(s.artist, " - ")
	if titleHasSep {
		ja, eng, _ := strings.Cut(s.title, " - ")
		jaTitle := fmt.Sprintf("%v %v", ja, s.artist)
		engTitle := fmt.Sprintf("%v %v", eng, s.artist)
		queries = append(queries, Query{q: jaTitle, message: "Searching using " + jaTitle + " as title for '&s'"})
		queries = append(queries, Query{q: engTitle, message: "Searching using " + engTitle + " as title for '&s'"})
	}
	if artistHasSep {
		ja, eng, _ := strings.Cut(s.artist, " - ")
		jaArtist := fmt.Sprintf("%v %v", s.title, ja)
		engArtist := fmt.Sprintf("%v %v", s.title, eng)
		queries = append(queries, Query{q: jaArtist, message: "Searching using " + jaArtist + " as artist for '&s'"})
		queries = append(queries, Query{q: engArtist, message: "Searching using" + engArtist + " as artist for '&s'"})
	}
	queries = append(queries, Query{q: s.title, message: "Searching using just song title for '&s' likely to be wrong"})
	return queries
}

func (query *Query) URLSafeQuery() (string, error) {
	if query.q+query.title == "" {
		return "", AddError(fmt.Errorf("'q' and 'title' are empty"))
	}
	var f strings.Builder
	for i, v := range [4]string{query.q, query.title, query.artist, query.album} {
		if v == "" {
			continue
		}
		escapedString := url.QueryEscape(v)
		switch i {
		case 0:
			f.WriteString("q=")
			f.WriteString(escapedString)
			f.WriteString("&")
		case 1:
			f.WriteString("title=")
			f.WriteString(escapedString)
			f.WriteString("&")
		case 2:
			f.WriteString("artist=")
			f.WriteString(escapedString)
			f.WriteString("&")
		case 3:
			f.WriteString("album=")
			f.WriteString(escapedString)
			f.WriteString("&")
		}
	}
	return f.String(), nil
}

func (query *Query) fetchSearchResults() ([]byte, error) {
	url, err := query.URLSafeQuery()
	if err != nil {
		return []byte{}, AddError(err)
	}
	resp, err := http.Get(lrclibAPI + url)
	if err != nil {
		return []byte{}, AddError(err)
	}
	var leng = 1024
	if resp.ContentLength >= 0 {
		leng = int(resp.ContentLength)
	}
	b := make([]byte, leng)
	results := make([]byte, 0, leng)
	for {
		n, err := resp.Body.Read(b)
		if err != nil && err != io.EOF {
			return []byte{}, err
		} else if err == io.EOF {
			if n > 0 {
				results = append(results, b[:n]...)
			}
			break
		} else {
			results = append(results, b[:n]...)
		}
	}
	return results, nil
}

func (query Query) String() string {
	var final strings.Builder
	if query.q != "" {
		final.WriteString("q: ")
		final.WriteString(query.q)
	}
	if query.title != "" {
		final.WriteString("title: ")
		final.WriteString(query.title)
	}
	if query.artist != "" {
		final.WriteString("artist: ")
		final.WriteString(query.artist)
	}
	if query.album != "" {
		final.WriteString("album: ")
		final.WriteString(query.album)
	}
	return fmt.Sprintf("\n%v\n", final.String())
}

func (query *Query) UserFacingMessageQueryWithAttemptNumber(attemptNumber int, title string) string {
	mess := strings.ReplaceAll(query.message, "&s", title)
	return fmt.Sprintf("%v[attempt %v] %v%v", BLUE, attemptNumber, mess, RESET)
}

type LrcLibMissing struct {
	song *Song
}

func (s LrcLibMissing) Error() string {
	return fmt.Sprintf("%v[fail] Unable to find lyrics for %v%v", RED, s.song.title, RESET)
}

func (s *Song) fetchSongSearchResults() ([]byte, error) {
	empty := []byte("[]")
	for attempt, query := range s.makeQueries() {
		fmt.Println(query.UserFacingMessageQueryWithAttemptNumber(attempt+1, s.title))
		jsonResponse, err := query.fetchSearchResults()
		isEmpty := bytes.Equal(jsonResponse, empty)
		if !isEmpty && err == nil {
			return jsonResponse, nil
		} else if isEmpty && err == nil {
			continue
		} else if err != nil {
			return nil, AddError(err)
		}
	}
	return nil, LrcLibMissing{song: s}
}

func (sr *SearchResult) GetLyric(song Song) (Lyric, error) {
	if sr.PlainLyrics+sr.SyncedLyrics == "" {
		return Lyric{}, AddError(fmt.Errorf("no lyrics"))
	}
	if len(sr.SyncedLyrics) > 0 {
		return Lyric{lyrics: sr.SyncedLyrics, synced: true, song: song}, nil
	}
	return Lyric{lyrics: sr.PlainLyrics, synced: false, song: song}, nil
}

func processJson(jsonResult []byte, song Song) (Lyric, error) {
	searchResults := make([]SearchResult, 0)
	json.Unmarshal(jsonResult, &searchResults)
	var final Lyric
	var err error
	for _, result := range searchResults {
		final, err = result.GetLyric(song)
		if err != nil {
			continue
		}
		if final.synced {
			break
		}
	}
	emptyLyric := Lyric{}
	if final == emptyLyric {
		return emptyLyric, AddError(fmt.Errorf("no lyrics found"))
	}
	return final, nil
}

func readCsv(fileName string) (*csv.Reader, error) {
	fileReader, err := os.Open(fileName)
	if err != nil {
		return csv.NewReader(nil), AddError(err)
	}
	reader := csv.NewReader(fileReader)
	return reader, nil
}

func (s *Song) GetLyricsForSong(directory string) {
	lrc, err := isLrcLibReachable()
	if !(lrc) {
		fmt.Printf("%v[internet] Unable to reach lrclib.net%v\n", RED, RESET)
		return
	}
	fileName := s.GetPathForSong(directory)
	info, err := os.Stat(fileName)
	if err == nil {
		if info.Size() > 0 {
			fmt.Printf("%v[skip] non-empty file exists for %v%v\n", MAGENTA, s.title, RESET)
			return
		}
	}
	fmt.Printf("%v[start] Searching for %v%v\n", BLUE, s.title, RESET)
	jsonResult, err := s.fetchSongSearchResults()
	if e, ok := err.(LrcLibMissing); ok {
		fmt.Println(e.Error())
		return
	} else if err != nil {
		fmt.Printf("Couldn't search lyrics for %v: %v\n", s.title, err.Error())
		return
	}
	lyric, err := processJson(jsonResult, *s)
	if err != nil {
		fmt.Printf("Couldn't process the json array for %v: %v\n", s.title, err.Error())
		return
	}
	syn := "unknown"
	if lyric.synced {
		syn = "synced"
	} else {
		syn = "plain"
	}
	fmt.Printf("%v[processed] Successfully found %v lyrics for '%v'%v\n", GREEN, syn, s.title, RESET)
	err = lyric.SaveToFile(directory)
	if err != nil {
		fmt.Printf("%v[file] Couldn't save the file for '%v': %v\n%v", RED, s.title, err.Error(), RESET)
		return
	}
	fmt.Printf("%v[file] Saved to '%v'\n%v", GREEN, fileName, RESET)
}

func validateFile(fileName string) {
	info, err := os.Stat(fileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("%v[existence] file %v does not exist%v\n", RED, fileName, RESET)
			os.Exit(2)
		} else if errors.Is(err, fs.ErrPermission) {
			fmt.Printf("%v[permission] denied%v\n", RED, RESET)
			os.Exit(2)
		}
	}
	if info.IsDir() {
		fmt.Printf("%v[directory] %v is a directory%v\n", RED, fileName, RESET)
		os.Exit(2)
	}
}

// Source - https://stackoverflow.com/a/42227115
// Posted by Tinwor, modified by community. See post 'Timeline' for change history
// Retrieved 2026-07-05, License - CC BY-SA 4.0

func isLrcLibReachable() (bool, error) {
	timeout := 3 * time.Second
	_, err := net.DialTimeout("tcp", "lrclib.net:https", timeout)
	if err != nil {
		return false, err
	}
	return true, nil
}

func main() {
	arguments := os.Args
	if len(arguments) < 2 {
		fmt.Printf("Usage: %v [csv file]\n", arguments[0])
		os.Exit(1)
	}
	csvFile := arguments[1]
	validateFile(csvFile)
	r, err := readCsv(csvFile)
	if err != nil {
		fmt.Printf("Couldn't read %v: \n%v\n", arguments[1], err.Error())
		os.Exit(1)
	}
	dirName := strings.ReplaceAll(arguments[1], ".csv", "") + "_lyrics"
	_, _ = r.Read()
	pool, err := ants.NewPool(4)
	if err != nil {
		fmt.Printf("Couldn't initialize a pool: \n%v\n", err.Error())
		os.Exit(1)
	}
	var dirExistNot = true
	for {
		s, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Printf("Couldn't get csv record: \n%v\n", err.Error())
			break
		}
		song, err := SongFromSlice(s)
		if err != nil {
			fmt.Printf("Couldn't turn csv record into Song: \n%v\n", err.Error())
			continue
		}
		if dirExistNot {
			err = os.MkdirAll(dirName, os.ModeDir|os.ModePerm)
			if err != nil {
				fmt.Printf("%v[creation] Couldn't make directory: \n%v%v\n", RED, err.Error(), RESET)
				os.Exit(2)
			}
			dirExistNot = false
		}
		pool.Submit(func() { song.GetLyricsForSong(dirName) })
	}
	for pool.Running() > 1 {
		time.Sleep(time.Second * 15)
	}
}

package main

// scanning an HTTP response for phrases

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"code.google.com/p/mahonia"
	"compress/gzip"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

var contentPhraseList = newPhraseList()

// scanContent scans the content of a document for phrases,
// and updates tally.
func scanContent(content []byte, contentType, charset string, tally map[rule]int) {
	if strings.Contains(contentType, "javascript") {
		scanJSContent(content, tally)
		return
	}

	decode := mahonia.NewDecoder(charset)
	if decode == nil {
		log.Printf("Unsupported charset (%s)", charset)
		decode = mahonia.NewDecoder("utf-8")
	}
	entities := strings.Contains(contentType, "html")

	ps := newPhraseScanner(contentPhraseList, func(s string) {
		tally[rule{t: contentPhrase, content: s}]++
	})
	ps.scanByte(' ')
	prevRune := ' '
	var buf [4]byte // buffer for UTF-8 encoding of runes

loop:
	for len(content) > 0 {
		var c rune
		if entities && content[0] == '&' {
			// Try to decode a character entity.
			entityLen := 1
			for entityLen < 32 && entityLen < len(content) {
				if b := content[entityLen]; 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9' || entityLen == 1 && b == '#' {
					entityLen++
				} else {
					break
				}
			}
			if entityLen < len(content) && content[entityLen] == ';' {
				entityLen++
			}
			e := string(content[:entityLen])
			decoded := html.UnescapeString(e)
			if decoded == e {
				// It isn't a valid entity; just read it as an ampersand.
				c = '&'
				content = content[1:]
			} else {
				unchanged := 0
				for unchanged < len(e) && unchanged < len(decoded) {
					if e[len(e)-unchanged-1] == decoded[len(decoded)-unchanged-1] {
						unchanged++
					} else {
						break
					}
				}
				c = []rune(decoded)[0]
				content = content[len(e)-unchanged:]
			}
		} else {
			// Read one Unicode character from content.
			r, size, status := decode(content)
			content = content[size:]
			switch status {
			case mahonia.STATE_ONLY:
				continue
			case mahonia.NO_ROOM:
				break loop
			}
			c = r
		}

		// Simplify it to lower-case words separated by single spaces.
		c = wordRune(c)
		if c == ' ' && prevRune == ' ' {
			continue
		}
		prevRune = c

		// Convert it to UTF-8 and scan the bytes.
		if c < 128 {
			ps.scanByte(byte(c))
			continue
		}
		n := utf8.EncodeRune(buf[:], c)
		for _, b := range buf[:n] {
			ps.scanByte(b)
		}
	}

	ps.scanByte(' ')
}

// scanJSContent scans only the contents of quoted JavaScript strings
// in the document.
func scanJSContent(content []byte, tally map[rule]int) {
	_, items := lex(string(content))
	ps := newPhraseScanner(contentPhraseList, func(s string) {
		tally[rule{t: contentPhrase, content: s}]++
	})

	for s := range items {
		s = wordString(s)
		ps.scanByte(' ')
		for i := 0; i < len(s); i++ {
			ps.scanByte(s[i])
		}
		ps.scanByte(' ')
	}
}

// responseContent reads the body of an HTTP response into a slice of bytes.
// It decompresses gzip-encoded responses.
func responseContent(res *http.Response) []byte {
	r := res.Body
	defer r.Close()

	if res.Header.Get("Content-Encoding") == "gzip" {
		gzContent, err := ioutil.ReadAll(r)
		if err != nil {
			log.Printf("error reading gzipped content for %s: %s", res.Request.URL, err)
			return nil
		}
		if len(gzContent) == 0 {
			// If the compressed content is empty, decompress it to empty content.
			return nil
		}
		gz, err := gzip.NewReader(bytes.NewBuffer(gzContent))
		if err != nil {
			log.Printf("could not create gzip decoder for %s: %s", res.Request.URL, err)
			return nil
		}
		defer gz.Close()
		r = gz
		res.Header.Del("Content-Encoding")
	}

	content, _ := ioutil.ReadAll(r)
	// Deliberately ignore the error. ebay.com searches produce errors, but work.

	return content
}

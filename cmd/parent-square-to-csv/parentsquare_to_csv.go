package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"syscall"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	psURL = "https://www.parentsquare.com"
)

type Student struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		ID               int         `json:"id"`
		FirstName        string      `json:"first_name"`
		LastName         string      `json:"last_name"`
		ExternalID       interface{} `json:"external_id"`
		Unlisted         bool        `json:"unlisted"`
		AssociatedUserID interface{} `json:"associated_user_id"`
		Email            interface{} `json:"email"`
		Phone            interface{} `json:"phone"`
	} `json:"attributes"`
	Relationships struct {
		Grade struct {
			Data struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"grade"`
		Parents struct {
			Data []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"parents"`
	} `json:"relationships"`
}

type Class struct {
	name string
	id   string
}

func main() {
	outputFileName := flag.String("o", "", "File to output csv to. Will output to stdout if not specified.")
	flag.Parse()

	var buffer *bufio.Writer

	if *outputFileName == "" {
		buffer = bufio.NewWriter(os.Stdout)
	} else {
		outputFile, err := os.Create(*outputFileName)
		if err != nil {
			log.Fatal(err)
		}
		buffer = bufio.NewWriter(outputFile)
	}

	// Get username and password
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Unable to read email: %v", err)
	}
	fmt.Print("Password: ")
	password, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		log.Fatalf("Unable to read password: %v", err)
	}
	fmt.Print("\n")

	// Set up client
	options := cookiejar.Options{}
	jar, err := cookiejar.New(&options)
	if err != nil {
		log.Fatal(err)
	}
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	client := http.Client{Jar: jar, Transport: tr}

	// Get signin page
	resp, err := client.Get(psURL + "/signin")
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected response code getting signin page: %v", resp.StatusCode)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()

	// Extract authenticity_token from signin page
	re := regexp.MustCompile(`\<meta name=\"csrf-token\" content=\"(?P<Token>.*)\" />`)
	matches := re.FindStringSubmatch(string(data))
	tokenIndex := re.SubexpIndex("Token")

	// login to sessions page
	resp, err = client.PostForm(psURL+"/sessions", url.Values{
		"authenticity_token": {matches[tokenIndex]},
		"session[password]":  {string(password)},
		"session[email]":     {string(email)},
		"commit":             {"Sign In"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected response code logging in to sessions page: %v", resp.StatusCode)
	}
	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()

	classes, err := getClassNames(client)
	if err != nil {
		log.Fatal(err)
	}

	// Get list of students per class
	buffer.WriteString("Name,Email,Email2,Group\n")
	for _, class := range classes {
		students, err := getPsStudentList(client, class.id)
		if err != nil {
			log.Fatal(err)
		}

		for _, student := range students {
			parentEmails, err := getParentEmails(client, student)
			if err != nil {
				log.Fatal(err)
			}

			buffer.WriteString(fmt.Sprintf("%s %s,%s,%s,%s\n", strings.ReplaceAll(student.Attributes.FirstName, "\"", "'"), student.Attributes.LastName, parentEmails[0], parentEmails[1], class.name))
		}

		buffer.Flush()
	}

	buffer.Flush()
}

func getClassNames(client http.Client) ([]Class, error) {
	resp, err := client.Get(psURL + "/schools/884/users")
	if err != nil {
		return nil, err
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	re := regexp.MustCompile(`<a class="directory-menu-list-item " href="\/schools\/884\/users\?name=.*section=([0-9]*)">\n                  <span class="directory-menu-list-item-name">\n                    (.*)`)
	matches := re.FindAllStringSubmatch(string(data), -1)

	var classes []Class
	for _, match := range matches {
		if strings.Contains(match[2], "Volunteer Leaders") ||
			strings.Contains(match[2], " All ") ||
			strings.Contains(match[2], "Incoming") ||
			strings.Contains(match[2], "more staff") {
			continue
		}
		classes = append(classes, Class{
			id:   match[1],
			name: match[2],
		})
	}

	return classes, nil
}

func getPsStudentList(client http.Client, id string) ([]Student, error) {
	resp, err := client.Get(psURL + "/api/v2/sections/" + id + "/students")
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	re := regexp.MustCompile(`\{\"data\":(?P<JSON>.*),\"included\":\[.*`)
	matches := re.FindStringSubmatch(string(data))
	jsonIndex := re.SubexpIndex("JSON")
	jsonStudents := matches[jsonIndex]

	var students []Student
	err = json.Unmarshal([]byte(jsonStudents), &students)
	if err != nil {
		return nil, err
	}

	return students, nil
}

func getParentEmails(client http.Client, student Student) ([]string, error) {
	var emails []string

	re := regexp.MustCompile(`.*mailto:(?P<EMAIL>.*)\">.*`)

	for _, parent := range student.Relationships.Parents.Data {
		data, err := getPsURI(client, "/schools/884/users/"+parent.ID)
		if err != nil {
			return nil, err
		}

		matches := re.FindStringSubmatch(string(data))
		if len(matches) > 0 {
			emailIndex := re.SubexpIndex("EMAIL")
			if emailIndex != -1 {
				emails = append(emails, matches[emailIndex])
			}
		}
	}

	for i := 2 - len(emails); i > 0; i-- {
		emails = append(emails, "")
	}

	return emails, nil
}

func getPsURI(client http.Client, uri string) ([]byte, error) {
	resp, err := client.Get(psURL + uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected response code getting \"%s\": %v", uri, resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

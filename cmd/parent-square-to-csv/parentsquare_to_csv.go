package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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
	debugDir := flag.String("debug-dir", "", "Directory to write debug responses to.")
	envFile := flag.String("env-file", ".env", "File to read ParentSquare credentials from. Set to empty to disable.")
	flag.Parse()

	if *envFile != "" {
		if err := loadEnvFile(*envFile); err != nil && !os.IsNotExist(err) {
			log.Fatal(err)
		}
	}

	var buffer *bufio.Writer
	var outputFile *os.File

	if *outputFileName == "" {
		buffer = bufio.NewWriter(os.Stdout)
	} else {
		var err error
		outputFile, err = os.Create(*outputFileName)
		if err != nil {
			log.Fatal(err)
		}
		buffer = bufio.NewWriter(outputFile)
	}

	email, password, err := readCredentials()
	if err != nil {
		log.Fatal(err)
	}

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
	client := http.Client{Jar: jar, Transport: tr, Timeout: 60 * time.Second}

	// Get signin page
	log.Println("Fetching signin page...")
	resp, err := client.Get(psURL + "/signin")
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected response code getting signin page: %v", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()

	// Extract authenticity_token from signin page
	re := regexp.MustCompile(`name="authenticity_token" value="(?P<Token>[^"]+)"`)
	matches := re.FindStringSubmatch(string(data))
	tokenIndex := re.SubexpIndex("Token")
	if tokenIndex == -1 || len(matches) <= tokenIndex {
		log.Fatal("Unable to find authenticity token on signin page")
	}

	// login to sessions page
	log.Println("Logging in...")
	resp, err = client.PostForm(psURL+"/sessions", url.Values{
		"utf8":               {"✓"},
		"authenticity_token": {matches[tokenIndex]},
		"session[password]":  {password},
		"session[email]":     {email},
		"commit":             {"Sign In"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected response code logging in to sessions page: %v", resp.StatusCode)
	}
	log.Printf("Login response path: %s\n", resp.Request.URL.Path)
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()
	if debugDir != nil && *debugDir != "" {
		if err := writeDebugFile(*debugDir, "login-response.html", data); err != nil {
			log.Fatal(err)
		}
	}
	verifiedMFA := false
	if resp.Request.URL.Query().Get("mfa_required") == "true" {
		log.Println("Verification code required.")
		if err := submitVerificationCode(client, resp.Request.URL, matches[tokenIndex]); err != nil {
			log.Fatal(err)
		}
		verifiedMFA = true
	}
	if !verifiedMFA && (strings.Contains(string(data), `id="signin-panel"`) || strings.Contains(string(data), "Please sign in.")) {
		log.Fatal("Login did not create an authenticated session. Check the credentials, SSO requirements, or whether ParentSquare requires an additional verification step.")
	}

	log.Println("Fetching class list...")
	classes, err := getClassNames(client, *debugDir)
	if err != nil {
		log.Fatal(err)
	}
	if len(classes) == 0 {
		log.Fatal("No classes found. ParentSquare may have changed the school directory page markup, or this account may not have access to school 884.")
	}
	log.Printf("Found %d classes\n", len(classes))

	// Get list of students per class
	_, _ = buffer.WriteString("Name,Email,Email2,Group\n")
	for _, class := range classes {
		log.Printf("Fetching students for %q...\n", class.name)
		students, err := getPsStudentList(client, class.id)
		if err != nil {
			log.Fatal(err)
		}

		for _, student := range students {
			parentEmails, err := getParentEmails(client, student)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Fprintf(buffer, "%s %s,%s,%s,%s\n", strings.ReplaceAll(student.Attributes.FirstName, "\"", "'"), student.Attributes.LastName, parentEmails[0], parentEmails[1], class.name)
		}

		buffer.Flush()
	}

	buffer.Flush()
	if outputFile != nil {
		if err := outputFile.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

func readCredentials() (string, string, error) {
	email := strings.TrimSpace(os.Getenv("PARENTSQUARE_EMAIL"))
	password := os.Getenv("PARENTSQUARE_PASSWORD")
	if email != "" && password != "" {
		return email, password, nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("unable to read email: %w", err)
	}
	email = strings.TrimSpace(email)

	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(syscall.Stdin)
	if err != nil {
		return "", "", fmt.Errorf("unable to read password: %w", err)
	}
	fmt.Print("\n")

	return email, string(passwordBytes), nil
}

func readVerificationCode() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Verification code: ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("unable to read verification code: %w", err)
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return "", fmt.Errorf("verification code cannot be empty")
	}

	return code, nil
}

func submitVerificationCode(client http.Client, responseURL *url.URL, csrfToken string) error {
	code, err := readVerificationCode()
	if err != nil {
		return err
	}

	query := responseURL.Query()
	contactMethod := query.Get("contact_method")
	if contactMethod == "" {
		contactMethod = "email"
	}
	contactValue := query.Get("contact_value")
	if contactValue == "" {
		return fmt.Errorf("ParentSquare requested verification, but did not include the contact value")
	}

	payload := map[string]string{
		"data_value": contactValue,
		"code":       code,
	}
	if contactMethod == "phone" {
		payload["phone"] = contactValue
	} else {
		payload["email"] = contactValue
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, psURL+"/mfa/submit", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("verification failed with response code %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var result struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unable to parse verification response: %w", err)
	}
	if result.RedirectURL != "" {
		redirectURL := result.RedirectURL
		if strings.HasPrefix(redirectURL, "/") {
			redirectURL = psURL + redirectURL
		}
		resp, err := client.Get(redirectURL)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected response code after verification redirect: %v", resp.StatusCode)
		}
	}

	log.Println("Verification succeeded.")
	return nil
}

func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}

	return nil
}

func getClassNames(client http.Client, debugDir string) ([]Class, error) {
	resp, err := client.Get(psURL + "/schools/884/users")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response code getting class list: %v", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if debugDir != "" {
		if err := writeDebugFile(debugDir, "class-list.html", data); err != nil {
			return nil, err
		}
	}

	re := regexp.MustCompile(`(?s)<a[^>]*href="/schools/884/users\?[^"]*section=([0-9]+)[^"]*"[^>]*>.*?<span[^>]*class="[^"]*directory-menu-list-item-name[^"]*"[^>]*>\s*([^<]+)`)
	matches := re.FindAllStringSubmatch(string(data), -1)

	classes := make([]Class, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(html.UnescapeString(match[2]))
		if strings.Contains(name, "Volunteer Leaders") ||
			strings.Contains(name, " All ") ||
			strings.Contains(name, "Incoming") ||
			strings.Contains(name, "more staff") {
			continue
		}
		classes = append(classes, Class{
			id:   match[1],
			name: name,
		})
	}

	return classes, nil
}

func writeDebugFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

func getPsStudentList(client http.Client, id string) ([]Student, error) {
	resp, err := client.Get(psURL + "/api/v2/sections/" + id + "/students")
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	re := regexp.MustCompile(`\{\"data\":(?P<JSON>.*),\"included\":\[.*`)
	matches := re.FindStringSubmatch(string(data))
	jsonIndex := re.SubexpIndex("JSON")
	if jsonIndex == -1 || len(matches) <= jsonIndex {
		return nil, fmt.Errorf("unable to parse student list for section %s", id)
	}
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
		return nil, fmt.Errorf("unexpected response code getting \"%s\": %v", uri, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

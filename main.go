package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	scp "github.com/bramvdbogaerde/go-scp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type StringAndBool struct {
	text      string
	boolValue bool
}

func getCommandInfo() map[string]StringAndBool {
	// Map from command to help text and indication if sub-domain value is required (via flag).
	return map[string]StringAndBool{
		"build":          {"Build the website, overwriting the selected-domain's file on server.", true},
		"deploy":         {"Build and upload the latest version of the website to selected sub-domain", true},
		"preview":        {"Start a local server for previewing the website.", false},
		"upload":         {"Upload the built website and host it at selected sub-domain.", true},
		"rollback":       {"Rollback the website to whatever was present before the last deploy.", true},
		"rotatecert":     {"Rotate the ssl (https) cert for supported domains.", false},
		"listcerts":      {"List all of the domains for which we can rotate certs.", false},
		"listsubdomains": {"List all of the sub-domains we support generation of web pages.", false},
		"help":           {"Print this help text.", false},
	}
}

func getSupportedSubDomains() map[string]StringAndBool {
	// Map of sub-domain values in CLI to path to store files on server and if we support certs renewal.
	return map[string]StringAndBool{
		"simon":      {"simon.duchastel.com", true},
		"mr":         {"mr.duchastel.com", true},
		"nicolas":    {"nicolas.duchastel.com", true},
		"pointbolin": {"pointbolin.com", true},
		"com":        {"duchastel.com", true},
		"org":        {"duchastel.org", true},
		"rentals":    {"rentals.duchastel.com", true},
	}
}

var command string
var subDomain string

func main() {
	flag.StringVar(&subDomain, "subdomain", "", "Directory where to put files on file server (for what sub-domain)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Error: did not specify command. Try 'help' command.")
		return
	}
	command := args[0]

	cmdInfo, found := getCommandInfo()[command]
	if !found {
		fmt.Println("Error: Must provide valid command. Try 'help' command.")
		return
	}

	if _, found := getSupportedSubDomains()[subDomain]; !found && cmdInfo.boolValue {
		fmt.Println("Error: command '" + command + "' requires sub-domain. Unknown subdomain: '" + subDomain + "'. Try 'listsubdomains' to see list.")
		return
	}

	// parse the args and execute relevant command
	var err error
	switch command {
	case "preview": // preview the website on a local server
		err = startServer()
	case "build": // build the website
		err = build()
	case "upload": // upload the website
		err = upload()
	case "deploy": // convenience command for build + upload
		if err = build(); err == nil {
			err = upload() // only upload if there was no error building
		}
	case "rollback": // deploy whatever was downloaded before the last deploy
		err = rollback()
	case "rotatecert": // rotate the ssl (https) cert for the website
		err = rotateCert()
	case "listsubdomains": // list all of the supported sub-domains
		printSubdomains()
	case "listcerts": // list all certs which we manage
		printCerts()
	case "help":
		printHelp()
	default:
		fmt.Println("Error: Invalid command '" + command + "'")
		fmt.Println()
		printHelp()
		err = errors.New("Invalid command")
	}
	if err != nil {
		fmt.Println(err)
	}
}

//////
// Commands
////////

func printSubdomains() {
	fmt.Println("Supported sub-domains:")
	fmt.Println()
	fmt.Println("   Each value to pass into 'subdomain' arg and corresponding dir to store files on server.")
	fmt.Println()
	for k, v := range getSupportedSubDomains() {
		fmt.Printf("  %-15s -> files go into %s directory on server.\n", k, v.text)
	}
}

func printCerts() {
	fmt.Println("Here are the sub-domains for which we can rotate the SSL certificate:")
	fmt.Println()
	for k, v := range getSupportedSubDomains() {
		fmt.Printf("  %-15s -> %s\n", k, v.text)
	}
}

func printHelp() {
	programName := os.Args[0] // first arg is program name
	fmt.Println("Usage: " + programName + " [command]")

	fmt.Println()

	fmt.Println("Commands:")
	for k, v := range getCommandInfo() {
		var extraReq string
		if v.boolValue {
			extraReq = " (requires sub-domain)"
		}
		fmt.Printf("  %-15s - %s%s\n", k, v.text, extraReq)
	}
}

// Starts the server and launches the browser to view it
func startServer() error {
	fmt.Println("Starting local preview server...")

	cmd := exec.Command("hugo", "server")
	if err := cmd.Start(); err != nil {
		fmt.Println("Error: cannot run command 'hugo server'")
		return err
	}

	if err := exec.Command("x-www-browser", "http://localhost:1313").Run(); err != nil {
		fmt.Println("Error: cannot run command 'x-www-browser http://localhost:1313'")
		return err
	}

	if err := cmd.Wait(); err != nil {
		fmt.Println("Error: cannot run command 'hugo server'")
		return err
	}

	return nil
}

// Build the website, which places it in the public/ directory
func build() error {
	// clear the public/ directory to ensure clean build
	// however, deleting the directory itself causes issues so recreate
	// it after
	fmt.Println("Clearing " + HUGO_BUILD_DIRECTORY + " directory")
	if err := os.RemoveAll(HUGO_BUILD_DIRECTORY); err != nil {
		fmt.Println("Error: failed to clear " + HUGO_BUILD_DIRECTORY + " directory")
		return err
	}
	if err := os.Mkdir(HUGO_BUILD_DIRECTORY, os.ModePerm); err != nil {
		fmt.Println("Error: failed to create " + HUGO_BUILD_DIRECTORY + " directory")
	}

	// build the website
	fmt.Println("Building website")
	if err := exec.Command("hugo").Run(); err != nil {
		fmt.Println("Error: cannot run command 'hugo'")
		return err
	}

	return nil
}

// Upload the website to the web host
func upload() error {
	fmt.Println("Connecting to web host")
	// get the login configuration
	config, err := getSshClientConfig()
	if err != nil {
		return err
	}

	// start the connection to the web host
	client, err := ssh.Dial("tcp", config.tcpAddress, config.clientConfig)
	if err != nil {
		fmt.Println("Error: failed to connect to web host")
		return err
	}
	defer client.Close()

	// where the website is located on the web host
	websiteRoot := websiteRemoteRoot(config.clientConfig.User)

	fmt.Println("Copying old website from web host to " + SITE_OLD_DIRECTORY + " in case there are any issues")
	if err = downloadOldSite(websiteRoot, SITE_OLD_DIRECTORY, client); err != nil {
		return err
	}

	if err = uploadWebsite(websiteRoot, HUGO_BUILD_DIRECTORY, client); err != nil {
		return err
	}

	return nil
}

// Deploy whatever was downloaded before the last deploy.
// Before each deploy we download the old website to
// a backup folder - use this command to redeploy that.
func rollback() error {
	fmt.Println("Beginning rollback of old website in " + SITE_OLD_DIRECTORY)
	fmt.Println("Connecting to web host")
	// get the login configuration
	config, err := getSshClientConfig()
	if err != nil {
		return err
	}

	// start the connection to the web host
	client, err := ssh.Dial("tcp", config.tcpAddress, config.clientConfig)
	if err != nil {
		fmt.Println("Error: failed to connect to web host")
		return err
	}
	defer client.Close()

	// where the website is located on the web host
	websiteRoot := websiteRemoteRoot(config.clientConfig.User)

	// upload the backup website
	if err = uploadWebsite(websiteRoot, SITE_OLD_DIRECTORY, client); err != nil {
		return err
	}

	return nil
}

// Rotate the ssl (https) cert for the supported domains: simon.duchastel.com, duchastel.com, and
// duchastel.org domains
func rotateCert() error {
	fmt.Println("Command not yet implemented. Sorry!")

	return errors.New("Not yet implemented")
}

//////
// Helpers
////////

// Helper struct to hold all ssh config information
type sshConfig struct {
	clientConfig *ssh.ClientConfig
	tcpAddress   string
}

// Get ssh config from local ssh.config file
// ssh.config file MUST NOT be source-controlled (contains
// sensitive info like username/password)
func getSshClientConfig() (*sshConfig, error) {
	configFile, err := os.Open("ssh.config")
	defer configFile.Close()

	if err != nil {
		fmt.Println("Error: ssh config (username, password) must be provided in file ssh.config")
		fmt.Println("ssh.config format:")
		fmt.Println("- 1st line: username to auth into web host ssh")
		fmt.Println("- 2nd line: password to auth into web host ssh")
		fmt.Println("- 3rd line: tcp address in the format '[address]:[port]' (ex. 'server.com:22')")
		fmt.Println("- 4th line: location of ssh known_hosts file OR 'insecure' if host key should not be validated (INSECURE)")
		return nil, err
	}

	fileScanner := bufio.NewScanner(configFile)
	fileScanner.Split(bufio.ScanLines)

	if !fileScanner.Scan() {
		fmt.Println("Error: 1st line of ssh.config must contain ssh username")
		return nil, errors.New("ssh config error")
	}
	username := fileScanner.Text()

	if !fileScanner.Scan() {
		fmt.Println("Error: 2nd line of ssh.config must contain ssh password")
		return nil, errors.New("ssh config error")
	}
	password := fileScanner.Text()

	if !fileScanner.Scan() {
		fmt.Println("Error: 3rd line of ssh.config must contain tcp address in the format '[address]:[port]' (ex: 'server.com:22')")
		return nil, errors.New("ssh config error")
	}
	tcpAddress := fileScanner.Text()

	if !fileScanner.Scan() {
		fmt.Println("Error: 4th line of ssh.config must either be file location of ssh known_hosts file OR '" +
			INSECURE_MODE + "' if INSECURE mode should be used (no host key validation)")

		return nil, errors.New("ssh config error")
	}

	var hostKeyCallback ssh.HostKeyCallback
	knownHosts := fileScanner.Text()
	if knownHosts == INSECURE_MODE {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		var err error
		hostKeyCallback, err = knownhosts.New(knownHosts)
		if err != nil {
			fmt.Println("Error: problem parsing ssh known_hosts file")
			return nil, err
		}
	}

	return &sshConfig{
		&ssh.ClientConfig{
			User: username,
			Auth: []ssh.AuthMethod{
				ssh.Password(password),
			},
			HostKeyCallback: hostKeyCallback,
		},
		tcpAddress,
	}, nil
}

// Run a command on the remote host via ssh and return its output as a
// byte buffer
func runRemoteCommand(client *ssh.Client, command string) (*bytes.Buffer, error) {
	// start an interactive session
	session, err := client.NewSession()
	if err != nil {
		fmt.Println("Error: failed to create session")
		return nil, err
	}
	defer session.Close()

	// execute a command on the session
	var buffer bytes.Buffer
	session.Stdout = &buffer
	if err := session.Run(command); err != nil {
		fmt.Println("Error: failed to run command '" + command + "'")
		return nil, err
	}

	return &buffer, nil
}

// Run a command on the remote host via ssh and print its output to console
func runRemoteCommandToConsole(client *ssh.Client, command string) error {
	buffer, err := runRemoteCommand(client, command)
	if err != nil {
		return err
	}
	fmt.Println(buffer.String())

	return nil
}

// Returns true if the remote file is confirmed to
// be a file, false otherwise
// Implemented by running the `test -f` command remotely
func remoteFileIsFile(client *ssh.Client, filePath string) (bool, error) {
	buffer, err := runRemoteCommand(client, "test -f "+filePath+" && echo true || echo false")
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(buffer.String()) == "true", nil
}

// Returns true if the remote file is confirmed to
// be a directory, false otherwise.
// Implemented by running the `test -d` command remotely.
func remoteFileIsDirectory(client *ssh.Client, filePath string) (bool, error) {
	buffer, err := runRemoteCommand(client, "test -d "+filePath+" && echo true || echo false")
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(buffer.String()) == "true", nil
}

// Upload a file with the given ssh client.
// [sourceFilePath] is the path to the file (including filename)
// [destinationFilePath] is the path to the file on the remote host (including filename)
func uploadFile(client *ssh.Client, sourceFilePath, destinationFilePath string) error {
	scpClient, err := scp.NewClientBySSH(client)
	if err != nil {
		fmt.Println("Error: failed to create file-transfer client")
		return err
	}

	if err := scpClient.Connect(); err != nil {
		fmt.Println("Error: failed to create file-transfer connection over ssh")
		return err
	}
	defer scpClient.Close()

	// open the file to transfer
	fileToUpload, err := os.Open(sourceFilePath)
	if err != nil {
		fmt.Println("Error: unable to open file '" + sourceFilePath + "'")
		return err
	}
	defer fileToUpload.Close()

	// create all transitive directories
	if err := createDirAllRemote(client, filepath.Dir(destinationFilePath)); err != nil {
		return err
	}

	// copy the file to the remote host
	if err := scpClient.CopyFromFile(context.Background(), *fileToUpload, destinationFilePath, READ_ONLY_FILE); err != nil {
		fmt.Println("Error: failed to copy file ' " + sourceFilePath + "' to remote server")
		return err
	}

	return nil
}

// Create the directory [dirPath], including all transitive directories
// which do not yet exist, on the remote host
func createDirAllRemote(client *ssh.Client, dirPath string) error {
	allSubDirectories := strings.Split(dirPath, "/")
	directoryToCreate := ""
	for _, dir := range allSubDirectories {
		cleanedDir := strings.TrimSpace(dir)
		if len(cleanedDir) <= 0 {
			continue // skip any empty strings
		}
		directoryToCreate = directoryToCreate + "/" + cleanedDir

		// '|| echo true' ignores the exit code error and defaults to success (0) if mkdir fails
		_, err := runRemoteCommand(client, "mkdir "+directoryToCreate+" || echo true")
		if err != nil {
			fmt.Println("Error: failed to create transitive directories for file '" + dirPath + "'")
			return err
		}
	}
	return nil
}

// Download a file with the given ssh client. Downloads the file from
// [remoteFileLocation] and saves it as [destinationFileLocation] locally
// (destination location must include filename).
func downloadRemoteFile(client *ssh.Client, remoteFileLocation, destinationFileName string) error {
	scpClient, err := scp.NewClientBySSH(client)
	if err != nil {
		fmt.Println("Error: failed to create file-transfer client")
		return err
	}

	if err := scpClient.Connect(); err != nil {
		fmt.Println("Error: failed to create file-transfer connection over ssh")
		return err
	}
	defer scpClient.Close()

	file, err := createFileWithDirectories(destinationFileName)
	defer file.Close()

	scpClient.CopyFromRemote(context.Background(), file, remoteFileLocation)

	return nil
}

// List all files, including hidden ones (but not directories) within
// the remote directory specified by [remoteDirectoryPath]. Includes
// recursive files, ie. listing remote files in /foo will list /foo/bar/baz.txt
func listRemoteFiles(client *ssh.Client, remoteDirectoryPath string) ([]string, error) {
	buffer, err := runRemoteCommand(client, "find "+remoteDirectoryPath+" -type f")
	if err != nil {
		return nil, err
	}
	if buffer == nil || buffer.Len() <= 0 {
		return nil, nil // if buffer is nil or empty, no files were found
	}

	// each file is on its own line
	// ignore empty strings and recurse on directories
	files := strings.Split(buffer.String(), "\n")
	var filesToReturn []string
	for _, file := range files {
		// clean up the file name and skip any blank files
		trimmedFile := strings.TrimSpace(file)
		if len(trimmedFile) <= 0 {
			continue
		}
		filesToReturn = append(filesToReturn, trimmedFile)
	}

	return filesToReturn, nil
}

// Create the file if it does not exist, as well as all
// intermediate directories if they do not exist
func createFileWithDirectories(filePath string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	if err != nil {
		fmt.Println("Error: unable to create directories for '" + filePath + "'")
		return nil, err
	}
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error: unable to create file '" + filePath + "'")
		return nil, err
	}

	return file, nil
}

func downloadOldSite(remoteWebsiteRoot, oldSiteDownloadLocation string, sshClient *ssh.Client) error {
	// copy old website to bin/site-old/ as a back-up in case there's some kind of issue
	files, err := listRemoteFiles(sshClient, remoteWebsiteRoot)
	if err != nil {
		fmt.Println("Error: could not recursively list files from '" + remoteWebsiteRoot + "'")
		return err
	}

	// clear old website directory in preparation for storing old website
	if err := os.RemoveAll(oldSiteDownloadLocation); err != nil {
		fmt.Println("Error: cannot clear '" + oldSiteDownloadLocation + "' directory")
		return err
	}

	if len(files) <= 0 {
		fmt.Println("  Nothing to download")
	}
	for _, file := range files {
		destinationFile := oldSiteDownloadLocation + "/" + strings.TrimPrefix(file, remoteWebsiteRoot+"/")

		fmt.Println("  Downloading " + file)
		if err := downloadRemoteFile(sshClient, file, destinationFile); err != nil {
			return err
		}
	}
	return nil
}

func uploadWebsite(remoteWebsiteRoot, siteToUploadLocation string, sshClient *ssh.Client) error {
	fmt.Println("Removing old website from web host")
	// run 'rm -rf' to delete everything in the website directory
	if _, err := runRemoteCommand(sshClient, "rm -rf "+remoteWebsiteRoot+"/*"); err != nil {
		return err
	}

	fmt.Println("Uploading website to web host")
	if err := filepath.WalkDir(siteToUploadLocation, func(path string, file fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Println("Error: failed to read '" + path + "'")
			return walkErr
		}

		if !file.IsDir() && len(path) > 0 {
			// note that we don't need to localize the file path on the remote system because we know it's unix (ie. uses '/')
			uploadFilePath := remoteWebsiteRoot + "/" + strings.TrimPrefix(path, siteToUploadLocation+"/")
			pathOsLocalized := filepath.FromSlash(path) // localize the path to whatever separator is used on this system
			fmt.Println("  Uploading " + path)
			if err := uploadFile(sshClient, pathOsLocalized, uploadFilePath); err != nil {
				fmt.Println("Error: failed to upload file '" + path + "'")
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

////////
// Constants
////////

// Flag to use in config to instruct insecure ssh connection
const INSECURE_MODE = "insecure"

// Flag to use for setting file as read-only on the file system
const READ_ONLY_FILE = "0644"

// Location to store old website as backup while uploading/deploying new website
const SITE_OLD_DIRECTORY = "bin/website-old"

// Location of the Hugo build output directory
const HUGO_BUILD_DIRECTORY = "public"

// root of the website, ie. /home/[username]/public_html/simon.duchastel.com
func websiteRemoteRoot(username string) string {
	return "/home/" + username + "/public_html/" + getSupportedSubDomains()[subDomain].text
}

package consul

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/spf13/viper"
)

type ByLength []string

func (s ByLength) Len() int {
	return len(s)
}

func (s ByLength) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByLength) Less(i, j int) bool {
	return len(s[i]) > len(s[j])
}

func getConsul() *consulapi.Client {
	addr := viper.GetString("addr")
	token := viper.GetString("token")
	auth := viper.GetString("auth")
	ssl := viper.GetString("ssl")

	verbose := viper.GetBool("verbose")

	if verbose {
		fmt.Printf("Connecting to %s %s %s %s\n", addr, token, auth, ssl)
	}
	config := consulapi.DefaultConfig()
	config.Address = addr

	if ssl == "true" {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		config.HttpClient = &http.Client{Transport: transport}
		config.Scheme = "https"
	} else {
		config.Scheme = "http"
	}

	if auth != "" {
		sliceAuth := strings.Split(auth, ":")
		if len(sliceAuth) != 2 {
			fmt.Fprintln(os.Stderr, "Invalid AUTH string specified.")
			os.Exit(132)
		}
		user := sliceAuth[0]
		pass := sliceAuth[1]
		config.HttpAuth = &consulapi.HttpBasicAuth{Username: user, Password: pass}
	}

	if token != "" {
		config.Token = token
	}

	consul, _ := consulapi.NewClient(config)
	return consul
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// Check if path is unique and is not contained by another path
func pathIsUnique(s []string, path string) bool {
	for _, p := range s {
		if p != path && strings.HasPrefix(path, p) {
			return false
		}
	}
	return true
}

func pathsToQuery(paths []string) []string {
	sort.Sort(ByLength(paths))

	var uniquePaths []string

	for _, path := range paths {
		path = strings.Trim(path, "/")
		if pathIsUnique(paths, path) && !contains(uniquePaths, path) {
			uniquePaths = append(uniquePaths, path)
		}
	}

	return uniquePaths
}

func processEnv(envMap map[string]map[string]string, envKeys []string) {
	paths := viper.GetStringSlice("path")
	export := viper.GetBool("export")
	jsonExport := viper.GetBool("json")
	verbose := viper.GetBool("verbose")

	var keys []string
	env := make(map[string]string)

	for _, path := range paths {
		path = strings.Trim(path, "/")
		if _, ok := envMap[path]; ok {
			for k, v := range envMap[path] {
				if !contains(keys, k) {
					keys = append(keys, k)
					env[k] = v
				}
			}
		}
	}

	fi, _ := os.Stdout.Stat()
	if jsonExport {
		j, err := json.Marshal(env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating JSON: %s\n", err)
		} else {
			fmt.Println(string(j))
		}
	} else {
		for _, k := range keys {
			v := env[k]
			var envLine string
			if !strings.HasPrefix(v, "\"") && !strings.HasPrefix(v, "'") && !strings.HasSuffix(v, "\"") && !strings.HasSuffix(v, "'") {
				v = fmt.Sprintf("\"%s\"", v)
			}
			if export {
				envLine = fmt.Sprintf("export %s=%s", k, v)
			} else {
				envLine = fmt.Sprintf("%s=%s", k, v)
			}
			fmt.Printf("%s\n", envLine)
			if verbose && (fi.Mode()&os.ModeCharDevice) == 0 {
				fmt.Fprintf(os.Stderr, "%s\n", envLine)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "-- %d env variables loaded --\n", len(env))
}

func Keys() {
	paths := viper.GetStringSlice("path")
	verbose := viper.GetBool("verbose")

	consul := getConsul()

	uniquePaths := pathsToQuery(paths)

	kv := consul.KV()

	for _, p := range uniquePaths {
		if verbose {
			fmt.Fprintln(os.Stderr, "Looking at", p)
		}
		keyPaths, qm, err := kv.Keys(p+"/", "/", nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err, qm)
			os.Exit(133)
		} else {
			for _, keyPath := range keyPaths {
				fmt.Println(keyPath)
			}
		}
	}
}

func Get() {
	paths := viper.GetStringSlice("path")
	verbose := viper.GetBool("verbose")

	consul := getConsul()

	uniquePaths := pathsToQuery(paths)

	kv := consul.KV()

	envMap := map[string]map[string]string{}
	envKeys := []string{}

	for _, p := range uniquePaths {
		if verbose {
			fmt.Fprintln(os.Stderr, "Looking at", p)
		}
		kvPairs, qm, err := kv.List(p, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err, qm)
			os.Exit(133)
		} else {
			for _, kvPair := range kvPairs {
				val := string(kvPair.Value)

				parts := strings.Split(kvPair.Key, "/")
				folder := strings.Join(parts[:len(parts)-1], "/")
				folder = strings.Trim(folder, "/")
				varName := parts[len(parts)-1]

				if varName != "" {
					if ok, _ := regexp.MatchString("^[A-Za-z0-9_]*$", varName); !ok {
						fmt.Fprintf(os.Stderr, "Invalid var: %s\n", varName)
					} else {
						if _, ok := envMap[folder]; !ok {
							envMap[folder] = make(map[string]string)
						}
						envMap[folder][varName] = val
						if !contains(envKeys, varName) {
							envKeys = append([]string{varName}, envKeys...)
						}
					}
				}
			}
		}
	}

	processEnv(envMap, envKeys)
}

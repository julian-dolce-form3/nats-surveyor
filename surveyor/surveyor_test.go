// Copyright 2019 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package surveyor is used to garner data from a NATS deployment for Prometheus
package surveyor

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	st "github.com/nats-io/nats-surveyor/test"
)

// Testing constants
const (
	clientCert         = "../test/certs/client-cert.pem"
	clientKey          = "../test/certs/client-key.pem"
	serverCert         = "../test/certs/server-cert.pem"
	serverKey          = "../test/certs/server-key.pem"
	caCertFile         = "../test/certs/ca.pem"
	defaultSurveyorURL = "http://127.0.0.1:7777/metrics"
)

func httpGetSecure(url string) (*http.Response, error) {
	tlsConfig := &tls.Config{}
	caCert, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("Got error reading RootCA file: %s", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tlsConfig.RootCAs = caCertPool

	cert, err := tls.LoadX509KeyPair(
		clientCert,
		clientKey)
	if err != nil {
		return nil, fmt.Errorf("Got error reading client certificates: %s", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return httpClient.Get(url)
}

func httpGet(url string) (*http.Response, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	return httpClient.Get(url)
}

func getTestOptions() *Options {
	o := GetDefaultOptions()
	o.Credentials = st.SystemCreds
	o.ListenAddress = "127.0.0.1"
	return o
}

// PollSurveyorEndpoint polls a surveyor endpoint for data
func PollSurveyorEndpoint(t *testing.T, url string, secure bool, expectedRc int) (string, error) {
	var resp *http.Response
	var err error

	if secure {
		resp, err = httpGetSecure(url)
	} else {
		resp, err = httpGet(url)
	}
	if err != nil {
		return "", fmt.Errorf("error from get: %v", err)
	}
	defer resp.Body.Close()

	rc := resp.StatusCode
	if rc != expectedRc {
		return "", fmt.Errorf("expected a %d response, got %d", expectedRc, rc)
	}
	if rc != 200 {
		return "", nil
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("got an error reading the body: %v", err)
	}
	return string(body), nil
}

func pollAndCheck(t *testing.T, url, result string) (string, error) {
	results, err := PollSurveyorEndpoint(t, url, false, http.StatusOK)
	if err != nil {
		return "", err
	}
	if !strings.Contains(results, result) {
		log.Printf("\n\nRESULTS: %s\n\n", results)
		return results, fmt.Errorf("response did not have NATS data")
	}
	return results, nil
}

func TestSurveyor_Basic(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// poll and check for basic core NATS output
	output, err := pollAndCheck(t, defaultSurveyorURL, "nats_core_mem_bytes")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}

	// check for route output
	if strings.Contains(output, "nats_core_route_recv_msg_count") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}

	// check for gateway output
	if strings.Contains(output, "nats_core_gateway_sent_bytes") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}

	// check for labels
	if strings.Contains(output, "nats_server_host") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	if strings.Contains(output, "nats_server_cluster") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	if strings.Contains(output, "nats_server_id") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	if strings.Contains(output, "nats_server_gateway_name") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	if strings.Contains(output, "nats_server_gateway_id") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	if strings.Contains(output, "nats_server_route_id") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}
	s.Stop()
}

func TestSurveyor_Reconnect(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// poll and check for basic core NATS output
	_, err = pollAndCheck(t, defaultSurveyorURL, "nats")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}

	sc.Servers[0].Shutdown()

	// this poll should fail...
	output, err := pollAndCheck(t, defaultSurveyorURL, "nats_core_mem_bytes")
	if strings.Contains(output, "nats_up 0") == false {
		t.Fatalf("output did not contain nats-up 0")
	}

	// poll and check for basic core NATS output, the next server should
	for i := 0; i < 5; i++ {
		output, err = pollAndCheck(t, defaultSurveyorURL, "nats_core_mem_bytes")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		t.Fatalf("Retries failed.")
	}
	if strings.Contains(output, "nats_up 1") == false {
		t.Fatalf("output did not contain nats-up 1")
	}
}

func TestSurveyor_NoSystemAccount(t *testing.T) {
	ns := st.StartBasicServer()
	defer ns.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	results, err := PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK)
	if err != nil {
		t.Fatalf("Couldn't poll exporter: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Should have recieved some non-nats data")
	}
	if strings.Contains(results, "nats_core_mem_bytes") {
		t.Fatalf(("Should NOT have NATS data"))
	}
}

func TestSurveyor_HTTPS(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()
	opts.CaFile = caCertFile
	opts.CertFile = serverCert
	opts.KeyFile = serverKey

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// Check that we CANNOT connect with http
	if _, err = PollSurveyorEndpoint(t, "http://127.0.0.1:7777/metrics", false, http.StatusOK); err == nil {
		t.Fatalf("didn't recieve an error")
	}
	// Check that we CAN connect with https
	if _, err = PollSurveyorEndpoint(t, "https://127.0.0.1:7777/metrics", true, http.StatusOK); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}
}

func TestSurveyor_UserPass(t *testing.T) {
	ns := st.StartBasicServer()
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.HTTPUser = "colin"
	opts.HTTPPassword = "secret"
	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	if _, err = PollSurveyorEndpoint(t, "http://colin:secret@127.0.0.1:7777/metrics", false, http.StatusOK); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://garbage:badpass@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://colin:badpass@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://foo:secret@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}
}

func TestSurveyor_NoServer(t *testing.T) {
	s, err := NewSurveyor(getTestOptions())
	defer func() {
		if s != nil {
			s.Stop()
		}
	}()

	if err == nil {
		t.Fatalf("didn't get expected error")
	}
}

func TestSurveyor_MissingResponses(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	sc.Servers[1].Shutdown()

	// poll and check for basic core NATS output
	_, err = pollAndCheck(t, defaultSurveyorURL, "nats_core_mem_bytes")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}
}

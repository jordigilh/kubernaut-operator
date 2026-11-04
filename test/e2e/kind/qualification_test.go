/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kind

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	"golang.org/x/net/html"
	appsv1 "k8s.io/api/apps/v1"
)

type qualificationSession struct {
	callback     *http.Response
	callbackBody string
	userInfo     map[string]any
	sid          string
	setCookies   []*http.Cookie
}

type qualificationEvidence struct {
	Issue       string                         `json:"issue"`
	Images      map[string]string              `json:"images"`
	Results     map[string]qualificationResult `json:"results"`
	GeneratedAt string                         `json:"generatedAt"`
}

type qualificationResult struct {
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func init() {
	if !qualificationEnabledFromEnvironment() {
		return
	}
	Describe("Issue 609 OAuth2 Proxy Console qualification", Label("issue-609"), Ordered, func() {
		var (
			ctx       context.Context
			client    *http.Client
			adapter   *qualificationIngress
			session   qualificationSession
			evidence  qualificationEvidence
			fixtureCA []byte
		)

		BeforeAll(func() {
			ctx = context.Background()
			fixture, err := activeManualTLSFixture()
			Expect(err).NotTo(HaveOccurred())
			fixtureCA = append([]byte(nil), fixture.caPEM...)
			evidence = qualificationEvidence{
				Issue:       "kubernaut-operator#609",
				Images:      QualificationImageReferences(qualificationKeycloakImage(), qualificationOperatorImage()),
				Results:     make(map[string]qualificationResult),
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			}

			By("deploying the real-component qualification CR")
			Expect(applyKubernautCR(ctx)).To(Succeed())
			Expect(waitForContractReady(ctx)).To(Succeed())
			for _, deployment := range []string{
				"kubernaut-console", "apifrontend", "kubernaut-agent", "authwebhook",
			} {
				_, err = kubectl(ctx, "rollout", "status", "deployment/"+deployment,
					"-n", kubernautNamespace, "--timeout=10m")
				Expect(err).NotTo(HaveOccurred(), deployment)
			}

			By("starting the test-owned HTTPS Console ingress adapter")
			adapter, err = startQualificationIngress(ctx)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func(cleanupCtx context.Context) { Expect(adapter.Close(cleanupCtx)).To(Succeed()) })
			jar, jarErr := cookiejar.New(nil)
			Expect(jarErr).NotTo(HaveOccurred())
			client = qualificationHTTPClient(fixtureCA, adapter)
			client.Jar = jar
		})

		AfterAll(func() {
			Expect(writeQualificationEvidence(evidence)).To(Succeed())
		})

		It(
			"E2E-CN-609-HTTP-001 [IA-2, SC-8, SC-23]: completes Keycloak login "+
				"and exposes only the configured claims",
			func() {
				var err error
				session, err = loginQualificationUser(ctx, client)
				Expect(err).NotTo(HaveOccurred())
				Expect(session.userInfo["user"]).To(Equal("sre-user"))
				Expect(session.userInfo["email"]).To(Equal("sre-user@kubernaut.ai"))
				Expect(session.userInfo["groups"]).To(ContainElement("sre"))
				claims, ok := session.userInfo["additionalClaims"].(map[string]any)
				Expect(ok).To(BeTrue(), "OAuth2 Proxy /oauth2/userinfo must expose additionalClaims")
				Expect(claims["iss"]).To(Equal(qualificationKeycloakIssuer))
				Expect(claims["sub"]).NotTo(BeEmpty())
				Expect(claims["sid"]).NotTo(BeEmpty())
				Expect(session.sid).To(Equal(claims["sid"]))
				Expect(session.callback).NotTo(BeNil())
				Expect(cookieHasSecureFlags(session.setCookies)).To(BeTrue())
				Expect(session.callback.Header.Get("Authorization")).To(BeEmpty())

				Expect(strings.ToLower(session.callbackBody)).NotTo(ContainSubstring("access_token"))
				evidence.Results["login-claims-cookies"] = qualificationResult{
					Passed: true,
					Detail: "Keycloak authorization-code login returned sre-user, group sre, iss/sub/sid, " +
						"and Secure HttpOnly SameSite=Lax session cookies",
				}
			})

		It(
			"E2E-CN-609-HTTP-002 [AC-3, IA-2, SC-8]: forwards the authenticated identity "+
				"and rejects forged proxy headers",
			func() {
				response, err := qualificationRequest(ctx, client, "/a2a/access",
					map[string]string{"X-Forwarded-User": "forged", "X-Forwarded-Email": "forged@example.invalid"})
				Expect(err).NotTo(HaveOccurred())
				body, bodyErr := qualificationResponseBody(response)
				Expect(bodyErr).NotTo(HaveOccurred())
				Expect(response.StatusCode).To(Equal(http.StatusOK))
				Expect(strings.ToLower(body)).NotTo(ContainSubstring("access_token"))

				forgedResponse, err := qualificationRequest(ctx, client, "/a2a/access",
					map[string]string{
						"Authorization":            "Bearer forged-token",
						"X-Forwarded-Access-Token": "Bearer forged-token",
						"X-Forwarded-User":         "forged",
						"X-Forwarded-Email":        "forged@example.invalid",
						"X-Auth-Request-User":      "forged",
					})
				Expect(err).NotTo(HaveOccurred())
				forgedBody, bodyErr := qualificationResponseBody(forgedResponse)
				Expect(bodyErr).NotTo(HaveOccurred())
				Expect(
					forgedResponse.StatusCode,
				).To(Equal(http.StatusOK), "the valid OAuth2 Proxy session must replace forged identity headers")
				Expect(strings.ToLower(forgedBody)).NotTo(ContainSubstring("forged@example.invalid"))

				unauthenticatedClient := qualificationHTTPClient(fixtureCA, adapter)
				unauthenticatedResponse, err := qualificationRequest(ctx, unauthenticatedClient, "/a2a/access",
					map[string]string{
						"Authorization":            "Bearer forged-token",
						"X-Forwarded-Access-Token": "Bearer forged-token",
						"X-Forwarded-User":         "forged",
					})
				Expect(err).NotTo(HaveOccurred())
				unauthenticatedBody, bodyErr := qualificationResponseBody(unauthenticatedResponse)
				Expect(bodyErr).NotTo(HaveOccurred())
				Expect(unauthenticatedResponse.StatusCode).NotTo(Equal(http.StatusOK))
				Expect(strings.ToLower(unauthenticatedBody)).NotTo(ContainSubstring("access_token"))

				mcpResponse, err := qualificationRequest(ctx, unauthenticatedClient, "/mcp", nil)
				Expect(err).NotTo(HaveOccurred())
				mcpBody, bodyErr := qualificationResponseBody(mcpResponse)
				Expect(bodyErr).NotTo(HaveOccurred())
				Expect(mcpResponse.StatusCode).NotTo(Equal(http.StatusOK))
				Expect(strings.ToLower(mcpBody)).NotTo(ContainSubstring("access_token"))
				evidence.Results["forwarding-and-forged-headers"] = qualificationResult{
					Passed: true,
					Detail: "authenticated A2A access succeeded; forged identity and bearer headers did not " +
						"create unauthenticated access; unauthenticated MCP was not 200",
				}
			})

		It("E2E-CN-609-HTTP-003 [IA-2, SC-23]: refreshes a live session without changing the CRD contract", func() {
			By("temporarily accelerating cookie-refresh on the deployed OAuth2 Proxy for this test only")
			Expect(scaleQualificationOperator(ctx, 0)).To(Succeed())
			defer func() {
				Expect(patchOAuth2ProxyCookieRefresh(ctx, "25m")).To(Succeed())
				Expect(scaleQualificationOperator(ctx, 1)).To(Succeed())
				Expect(waitForDeployment(ctx, "kubernaut-console")).To(Succeed())
			}()
			Expect(patchOAuth2ProxyCookieRefresh(ctx, "2s")).To(Succeed())
			Expect(waitForDeployment(ctx, "kubernaut-console")).To(Succeed())

			refreshObserved := false
			var refreshedCookies []*http.Cookie
			Expect(pollUntilSuccess(ctx, 30*time.Second, 1*time.Second, func() error {
				response, err := qualificationRequest(ctx, client, "/oauth2/userinfo", nil)
				if err != nil {
					return err
				}
				body, bodyErr := qualificationResponseBody(response)
				if bodyErr != nil {
					return bodyErr
				}
				if response.StatusCode != http.StatusOK {
					return fmt.Errorf("userinfo returned HTTP %d", response.StatusCode)
				}
				if len(response.Cookies()) == 0 {
					return fmt.Errorf("OAuth2 Proxy has not emitted a refreshed session cookie")
				}
				if strings.Contains(strings.ToLower(body), "access_token") {
					return fmt.Errorf("userinfo response contains an access_token field")
				}
				refreshedCookies = response.Cookies()
				refreshObserved = true
				return nil
			})).To(Succeed())
			Expect(refreshObserved).To(BeTrue())
			Expect(cookieHasSecureFlags(refreshedCookies)).To(BeTrue())
			evidence.Results["session-refresh"] = qualificationResult{
				Passed: true,
				Detail: "test-only deployment argument acceleration caused OAuth2 Proxy to emit a refreshed " +
					"Secure HttpOnly SameSite=Lax cookie",
			}
		})

		It("E2E-CN-609-HTTP-004 [IA-2, SC-8, SC-23]: clears logout state and rotates sid on the next login", func() {
			logoutResponse, err := qualificationRequest(ctx, client, "/oauth2/sign_out", nil)
			Expect(err).NotTo(HaveOccurred())
			logoutCookies := logoutResponse.Cookies()
			location := strings.TrimSpace(logoutResponse.Header.Get("Location"))
			_, _ = qualificationResponseBody(logoutResponse)
			if location != "" {
				providerLogout, getErr := qualificationRequestURL(ctx, client, http.MethodGet, location, nil, nil)
				Expect(getErr).NotTo(HaveOccurred())
				providerBody, bodyErr := qualificationResponseBody(providerLogout)
				Expect(bodyErr).NotTo(HaveOccurred())
				Expect(strings.ToLower(providerBody)).NotTo(ContainSubstring("access_token"))
			}
			Expect(hasExpiredSessionCookie(logoutCookies)).To(BeTrue())
			unauthenticated, err := qualificationRequest(ctx, client, "/", nil)
			Expect(err).NotTo(HaveOccurred())
			unauthenticatedBody, bodyErr := qualificationResponseBody(unauthenticated)
			Expect(bodyErr).NotTo(HaveOccurred())
			Expect(unauthenticated.StatusCode).NotTo(Equal(http.StatusOK))
			Expect(strings.ToLower(unauthenticatedBody)).NotTo(ContainSubstring("access_token"))

			rotated, err := loginQualificationUser(ctx, client)
			Expect(err).NotTo(HaveOccurred())
			Expect(rotated.sid).NotTo(Equal(session.sid))
			Expect(rotated.sid).NotTo(BeEmpty())
			evidence.Results["logout-and-sid-rotation"] = qualificationResult{
				Passed: true,
				Detail: "logout expired the OAuth2 Proxy cookie and a new Keycloak login returned a different sid",
			}
		})
	})
}

func loginQualificationUser(ctx context.Context, client *http.Client) (qualificationSession, error) {
	result := qualificationSession{}
	startURL := "https://" + qualificationConsoleHost + ":8843/"
	response, err := qualificationRequestURL(ctx, client, http.MethodGet, startURL, nil, nil)
	if err != nil {
		return result, err
	}
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
		_, _ = qualificationResponseBody(response)
		return result, fmt.Errorf("Console root returned HTTP %d instead of a redirect", response.StatusCode)
	}
	loginStart, err := qualificationAbsoluteLocation(startURL, response)
	if err != nil {
		return result, err
	}
	_, _ = qualificationResponseBody(response)

	response, err = qualificationRequestURL(ctx, client, http.MethodGet, loginStart, nil, nil)
	if err != nil {
		return result, err
	}
	providerURL, err := qualificationAbsoluteLocation(loginStart, response)
	if err != nil {
		_, _ = qualificationResponseBody(response)
		return result, err
	}
	_, _ = qualificationResponseBody(response)

	response, err = qualificationRequestURL(ctx, client, http.MethodGet, providerURL, nil, nil)
	if err != nil {
		return result, err
	}
	loginAction, fields, bodyErr := parseQualificationLoginForm(response)
	if bodyErr != nil {
		return result, bodyErr
	}
	form := url.Values{}
	for name, value := range fields {
		form.Set(name, value)
	}
	form.Set("username", "sre-user")
	form.Set("password", "password")
	_, _ = qualificationResponseBody(response)

	response, err = qualificationRequestURL(ctx, client, http.MethodPost, loginAction, strings.NewReader(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return result, err
	}
	callbackURL, err := qualificationAbsoluteLocation(loginAction, response)
	if err != nil {
		_, _ = qualificationResponseBody(response)
		return result, err
	}
	_, _ = qualificationResponseBody(response)

	callback, err := qualificationRequestURL(ctx, client, http.MethodGet, callbackURL, nil, nil)
	if err != nil {
		return result, err
	}
	result.callback = callback
	result.setCookies = callback.Cookies()
	if callback.StatusCode < http.StatusMultipleChoices || callback.StatusCode >= http.StatusBadRequest {
		_, _ = qualificationResponseBody(callback)
		return result, fmt.Errorf("OAuth2 Proxy callback returned HTTP %d instead of a redirect", callback.StatusCode)
	}
	finalURL, err := qualificationAbsoluteLocation(callbackURL, callback)
	if err != nil {
		_, _ = qualificationResponseBody(callback)
		return result, err
	}
	result.callbackBody, err = qualificationResponseBody(callback)
	if err != nil {
		return result, err
	}

	finalResponse, err := qualificationRequestURL(ctx, client, http.MethodGet, finalURL, nil, nil)
	if err != nil {
		return result, err
	}
	finalBody, err := qualificationResponseBody(finalResponse)
	if err != nil {
		return result, err
	}
	if finalResponse.StatusCode != http.StatusOK {
		return result, fmt.Errorf("authenticated Console root returned HTTP %d", finalResponse.StatusCode)
	}
	if strings.Contains(strings.ToLower(finalBody), "access_token") {
		return result, fmt.Errorf("authenticated Console document contains an access_token field")
	}

	userinfo, err := qualificationRequest(ctx, client, "/oauth2/userinfo", nil)
	if err != nil {
		return result, err
	}
	userinfoBody, err := qualificationResponseBody(userinfo)
	if err != nil {
		return result, err
	}
	if userinfo.StatusCode != http.StatusOK {
		return result, fmt.Errorf("OAuth2 Proxy userinfo returned HTTP %d", userinfo.StatusCode)
	}
	if strings.Contains(strings.ToLower(userinfoBody), "access_token") {
		return result, fmt.Errorf("OAuth2 Proxy userinfo contains an access_token field")
	}
	if err := json.Unmarshal([]byte(userinfoBody), &result.userInfo); err != nil {
		return result, fmt.Errorf("decoding OAuth2 Proxy userinfo: %w", err)
	}
	claims, ok := result.userInfo["additionalClaims"].(map[string]any)
	if !ok {
		return result, fmt.Errorf("OAuth2 Proxy userinfo additionalClaims is not an object")
	}
	result.sid, ok = claims["sid"].(string)
	if !ok || result.sid == "" {
		return result, fmt.Errorf("OAuth2 Proxy userinfo sid claim is empty")
	}
	return result, nil
}

func qualificationRequest(
	ctx context.Context, client *http.Client, path string, headers map[string]string,
) (*http.Response, error) {
	return qualificationRequestURL(
		ctx, client, http.MethodGet, "https://"+qualificationConsoleHost+":8843"+path, nil, headers,
	)
}

func qualificationRequestURL(
	ctx context.Context, client *http.Client, method, rawURL string, body io.Reader, headers map[string]string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return client.Do(request)
}

func qualificationAbsoluteLocation(base string, response *http.Response) (string, error) {
	location, err := qualificationLocation(response)
	if err != nil {
		return "", err
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parsing redirect base URL: %w", err)
	}
	redirectURL, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parsing redirect URL: %w", err)
	}
	return baseURL.ResolveReference(redirectURL).String(), nil
}

func parseQualificationLoginForm(response *http.Response) (string, map[string]string, error) {
	if response == nil {
		return "", nil, fmt.Errorf("Keycloak login response is nil")
	}
	if response.StatusCode != http.StatusOK {
		_, _ = qualificationResponseBody(response)
		return "", nil, fmt.Errorf("Keycloak login page returned HTTP %d", response.StatusCode)
	}
	document, err := html.Parse(response.Body)
	if err != nil {
		return "", nil, fmt.Errorf("parsing Keycloak login page: %w", err)
	}
	var form *html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if form != nil {
			return
		}
		if node.Type == html.ElementNode && node.Data == "form" {
			form = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	if form == nil {
		return "", nil, fmt.Errorf("Keycloak login page contains no form")
	}
	action := ""
	for _, attribute := range form.Attr {
		if attribute.Key == "action" {
			action = attribute.Val
		}
	}
	if action == "" {
		return "", nil, fmt.Errorf("Keycloak login form contains no action")
	}
	fields := make(map[string]string)
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "input" {
			name, value, inputType := "", "", ""
			for _, attribute := range node.Attr {
				switch attribute.Key {
				case "name":
					name = attribute.Val
				case "value":
					value = attribute.Val
				case "type":
					inputType = attribute.Val
				}
			}
			if name != "" && (inputType == "hidden" || inputType == "text") {
				fields[name] = value
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(form)
	base := response.Request.URL.String()
	syntheticResponse := &http.Response{
		Header:     http.Header{"Location": []string{action}},
		StatusCode: http.StatusFound,
	}
	resolved, err := qualificationAbsoluteLocation(base, syntheticResponse)
	if err != nil {
		return "", nil, err
	}
	return resolved, fields, nil
}

func cookieHasSecureFlags(cookies []*http.Cookie) bool {
	if len(cookies) == 0 {
		return false
	}
	for _, cookie := range cookies {
		if !strings.HasPrefix(cookie.Name, "_kubernaut_console") {
			continue
		}
		if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			return false
		}
		return true
	}
	return false
}

func hasExpiredSessionCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, "_kubernaut_console") &&
			(cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now()))) {
			return true
		}
	}
	return false
}

func scaleQualificationOperator(ctx context.Context, replicas int32) error {
	if _, err := kubectl(ctx, "scale", "deployment/"+operatorDeploymentName, "-n", operatorNamespace,
		"--replicas="+strconv.FormatInt(int64(replicas), 10)); err != nil {
		return fmt.Errorf("scaling operator to %d replicas: %w", replicas, err)
	}
	return pollUntilSuccess(ctx, 2*time.Minute, time.Second, func() error {
		output, err := kubectl(ctx, "get", "deployment/"+operatorDeploymentName, "-n", operatorNamespace,
			"-o", "jsonpath={.status.readyReplicas}")
		if err != nil {
			return err
		}
		ready := strings.TrimSpace(output)
		if replicas == 0 && (ready == "" || ready == "0") {
			return nil
		}
		if replicas > 0 && ready == strconv.FormatInt(int64(replicas), 10) {
			return nil
		}
		return fmt.Errorf("operator ready replicas=%q, wanted %d", ready, replicas)
	})
}

func patchOAuth2ProxyCookieRefresh(ctx context.Context, refresh string) error {
	output, err := kubectl(ctx, "get", "deployment/kubernaut-console", "-n", kubernautNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("reading Console Deployment: %w", err)
	}
	deployment := &appsv1.Deployment{}
	if err := json.Unmarshal([]byte(output), deployment); err != nil {
		return fmt.Errorf("decoding Console Deployment: %w", err)
	}
	for containerIndex, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "oauth2-proxy" {
			continue
		}
		for argumentIndex, argument := range container.Args {
			if !strings.HasPrefix(argument, "--cookie-refresh=") {
				continue
			}
			patch := fmt.Sprintf(
				`[{"op":"replace","path":"/spec/template/spec/containers/%d/args/%d",`+
					`"value":"--cookie-refresh=%s"}]`,
				containerIndex, argumentIndex, refresh,
			)
			if _, err := kubectl(
				ctx, "patch", "deployment/kubernaut-console", "-n", kubernautNamespace,
				"--type=json", "-p", patch,
			); err != nil {
				return fmt.Errorf("patching OAuth2 Proxy cookie refresh: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("Console Deployment has no OAuth2 Proxy cookie-refresh argument")
}

func waitForDeployment(ctx context.Context, name string) error {
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/"+name, "-n", kubernautNamespace, "--timeout=5m",
	); err != nil {
		return fmt.Errorf("waiting for %s Deployment: %w", name, err)
	}
	return nil
}

func writeQualificationEvidence(evidence qualificationEvidence) error {
	path := strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_QUALIFICATION_EVIDENCE"))
	if path == "" {
		path = filepath.Join(os.TempDir(), "kubernaut-issue-609-qualification.json")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating qualification evidence directory: %w", err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling qualification evidence: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing qualification evidence: %w", err)
	}
	return nil
}

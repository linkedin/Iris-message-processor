package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

const (
	userAgent              = "iris-message-processor"
	authPath               = "/v0/internal/auth/applications"
	userDetailsPath        = "/v0/users/"
	roleTargetPath         = "/v0/internal/target"
	incidentEscalationPath = "/v0/internal/incident/"
	planDetailsPath        = "/v0/plans/"
	applicationDetailsPath = "/v0/applications/"
	templateDetailsPath    = "/v0/templates/"
	buildMessagePath       = "/v0/internal/build_message"
	renderJinjaPath        = "/v0/internal/render_jinja"
	incidentsPath          = "/v0/internal/incidents/"
	planAggregationPath    = "/v0/internal/plan_aggregation_settings"
	priorityModeMapPath    = "/v0/internal/priority_mode_map"
	peerCountPath          = "/v0/internal/sender_peer_count"
	heartbeatPath          = "/v0/internal/sender_heartbeat/"
)

var (
	counterIrisApiClientErrors = gometrics.NewCounter()
)

type IncidentIDList struct {
	IDs []uint64 `json:"incident_ids"`
}

type IncidentStep struct {
	Step   int  `json:"current_step"`
	Active bool `json:"active"`
}

type AppAuth struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type MobileDevice struct {
	RegistrationID string `json:"registration_id"`
	UserID         uint64 `json:"user_id"`
	Platform       string `json:"platform"`
}
type UserInfo struct {
	Name              string                         `json:"name"`
	Admin             bool                           `json:"admin"`
	TemplateOverrides []string                       `json:"template_overrides"`
	CategoryOverrides map[string](map[string]string) `json:"category_overrides"`
	Modes             map[string]string              `json:"modes"`
	Devices           []MobileDevice                 `json:"device"`
	PerAppModes       map[string](map[string]string) `json:"per_app_modes"`
	Teams             []string                       `json:"teams"`
	Contacts          map[string]string              `json:"contacts"`
}

type RoleTargetResponse struct {
	Error string              `json:"error"`
	Users map[string]UserInfo `json:"users"`
}

type PlanAggregationResp struct {
	PlanID            uint64 `json:"id"`
	PlanName          string `json:"name"`
	ThresholdWindow   int    `json:"threshold_window"`
	ThresholdCount    int    `json:"threshold_count"`
	AggregationWindow int    `json:"aggregation_window"`
	AggregationReset  int    `json:"aggregation_reset"`
}

type Plan struct {
	PlanId            uint64                         `json:"id"`
	Name              string                         `json:"name"`
	ThresholdWindow   int                            `json:"threshold_window"`
	ThresholdCount    int                            `json:"threshold_count"`
	AggregationWindow int                            `json:"aggregation_window"`
	AggregationReset  int                            `json:"aggregation_reset"`
	Description       string                         `json:"description"`
	Created           int                            `json:"created"`
	Creator           string                         `json:"Creator"`
	Active            int                            `json:"active"`
	TrackingType      string                         `json:"tracking_type"`
	TrackingKey       string                         `json:"tracking_key"`
	TrackingTemplate  map[string](map[string]string) `json:"tracking_template"`
	Steps             []([]PlanStep)                 `json:"steps"`
}

type PlanStep struct {
	StepId       int    `json:"id"`
	StepNumber   int    `json:"step"`
	Repeat       int    `json:"repeat"`
	Wait         int    `json:"wait"`
	Optional     int    `json:"optional"`
	Role         string `json:"role"`
	Target       string `json:"target"`
	Template     string `json:"template"`
	Priority     string `json:"priority"`
	DynamicIndex int    `json:"dynamic_index"`
}

type NotificationCategory struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
}

type Application struct {
	Name                  string                 `json:"name"`
	ContextTemplate       string                 `json:"context_template"`
	SampleTemplate        string                 `json:"sample_context"`
	SummaryTemplate       string                 `json:"summary_template"`
	MobileTemplate        string                 `json:"mobile_template"`
	Variables             []string               `json:"variables"`
	RequiredVariables     []string               `json:"required_variables"`
	TitleVariable         string                 `json:"title_variable"`
	DefaultModes          map[string]string      `json:"default_modes"`
	SupportedModes        []string               `json:"supported_modes"`
	Owners                []string               `json:"owners"`
	CustomSenderAddresses map[string]string      `json:"custom_sender_addresses"`
	Categories            []NotificationCategory `json:"categories"`
}

type TemplatePlanReference struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

type Template struct {
	ID             uint64                                      `json:"id"`
	Name           string                                      `json:"name"`
	Active         int                                         `json:"active"`
	Creator        string                                      `json:"creator"`
	Created        int                                         `json:"created"`
	Content        map[string](map[string](map[string]string)) `json:"content"`
	PlanReferences []TemplatePlanReference                     `json:"plans"`
}

type Message struct {
	Application    string   `json:"application"`
	Target         string   `json:"target"`
	Destination    string   `json:"destination"`
	DeviceIDs      []string `json:"device_ids"`
	BCCDestination string   `json:"bcc_destination"`
	From           string   `json:"sender_address"`
	EmailHTML      string   `json:"email_html"`
	EmailText      string   `json:"email_text"`
	Mode           string   `json:"mode"`
	ModeID         int      `json:"mode_id"`
	Priority       string   `json:"priority"`
	PriorityID     int      `json:"priority_id"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	Template       string   `json:"template"`
	TemplateID     int      `json:"template_id"`
	PlanStep       int      `json:"step"`

	PlanStepNotificationId int // id of a specific notification within each step
	PlanStepIndex          int // index of a specific notification within each step
	MessageUUID            uuid.UUID
}

type Notification struct {
	Role           string                   `json:"role,omitempty"`
	RoleID         int                      `json:"role_id,omitempty"`
	Target         string                   `json:"target,omitempty"`
	TargetID       uint64                   `json:"target_id,omitempty"`
	Subject        string                   `json:"subject,omitempty"`
	Template       string                   `json:"template,omitempty"`
	Context        map[string]interface{}   `json:"context,omitempty"`
	Priority       string                   `json:"priority,omitempty"`
	PriorityID     int                      `json:"priority_id,omitempty"`
	Plan           string                   `json:"plan,omitempty"`
	PlanID         uint64                   `json:"plan_id,omitempty"`
	IncidentID     uint64                   `json:"incident_id,omitempty"`
	Application    string                   `json:"application,omitempty"`
	Destination    string                   `json:"destination,omitempty"`
	BCCDestination string                   `json:"bcc_destination,omitempty"`
	Mode           string                   `json:"mode,omitempty"`
	ModeID         int                      `json:"mode_id,omitempty"`
	Category       string                   `json:"category,omitempty"`
	CategoryID     int                      `json:"category_id,omitempty"`
	CategoryMode   string                   `json:"category_mode,omitempty"`
	CategoryModeID int                      `json:"category_mode_id,omitempty"`
	Body           string                   `json:"body,omitempty"`
	EmailHTML      string                   `json:"email_html,omitempty"`
	MultiRecipient bool                     `json:"multi-recipient,omitempty"`
	TemplateID     uint64                   `json:"template_id,omitempty"`
	DynamicIndex   int                      `json:"dynamic_index"`
	OptionalIndex  string                   `json:"optional,omitempty"`
	TargetList     []map[string]interface{} `json:"target_list,omitempty"`
	PlanStep       int                      `json:"step,omitempty"`

	LastSent               time.Time
	SentCount              int
	PlanStepNotificationId int // id of a specific notification within each step
	PlanStepIndex          int // index of a specific notification within each step
}

type Incident struct {
	IncidentID    uint64                 `json:"id"`
	Plan          string                 `json:"plan"`
	PlanID        uint64                 `json:"plan_id"`
	Active        int                    `json:"active"`
	Updated       int                    `json:"updated"`
	Application   string                 `json:"application"`
	Context       map[string]interface{} `json:"context"`
	Created       int                    `json:"created"`
	Owner         string                 `json:"owner"`
	CurrentStep   int                    `json:"current_step"`
	TitleVariable string                 `json:"title_variable_name"`
	Resolved      int                    `json:"resolved"`
	Title         string                 `json:"title"`
}

type IrisClient struct {
	baseurl     string
	application string
	key         []byte
	http        *http.Client
	logger      *Logger
	config      *Config
}

// RegisterMetrics register IrisClient metrics to provided metrics registry
func (c *IrisClient) RegisterMetrics(reg gometrics.Registry) {
	reg.Register("irisClient.irisApi.errors_total", counterIrisApiClientErrors)
}

// Get generic GET request with iris-api hmac authentication header added in
func (c *IrisClient) Get(url string, params map[string][]string, b []byte) (respBody []byte, err error) {

	fullURL := c.baseurl + url

	req, err := http.NewRequest(http.MethodGet, fullURL, bytes.NewReader(b))
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to create %s for %s: %v", http.MethodGet, fullURL, err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}

	if len(params) > 0 {
		q := req.URL.Query()
		for key, list := range params {
			for _, value := range list {
				q.Add(key, value)
			}
		}
		req.URL.RawQuery = q.Encode()

		// include query string in hmac hash
		url = url + "?" + req.URL.RawQuery
	}

	auth, _, err := AuthHeader(http.MethodGet, url, b, c.application, []byte(c.key))
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	req.Header.Add("User-Agent", userAgent)
	req.Header.Add("Authorization", auth)

	resp, err := c.http.Do(req)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to make request for %s: %v", req.URL.String(), err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to read response for %s: %v", req.URL.String(), err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}

	// accept 200, 201, 204 response codes
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		clientErr := fmt.Errorf("Received unexpected response code for %s: %d. Response body: %s", req.URL.String(), resp.StatusCode, string(body))
		if resp.StatusCode != http.StatusBadRequest {
			counterIrisApiClientErrors.Inc(1)
			c.logger.Errorf("irisClient err: %s", clientErr)
		} else {
			c.logger.Warnf("irisClient err: %s", clientErr)
		}
		return nil, clientErr
	}

	return body, nil

}

// Post generic POST request with iris-api hmac authentication header added in
func (c *IrisClient) Post(url string, jsonBody []byte) (respBody []byte, err error) {

	fullURL := c.baseurl + url
	req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to create %s for %s: %v", http.MethodGet, fullURL, err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}

	req.Header.Set("Content-Type", "application/json")

	auth, _, err := AuthHeader("POST", url, jsonBody, c.application, []byte(c.key))
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}
	req.Header.Add("User-Agent", userAgent)
	req.Header.Add("Authorization", auth)

	resp, err := c.http.Do(req)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to make request for %s: %v", req.URL.String(), err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		clientErr := fmt.Errorf("Failed to read response for %s: %v", req.URL.String(), err)
		c.logger.Errorf("irisClient err: %s", clientErr)
		return nil, clientErr
	}

	// accept 200, 201, 204 response codes
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		clientErr := fmt.Errorf("Received unexpected response code for %s: %d. Response body: %s", req.URL.String(), resp.StatusCode, string(body))
		if resp.StatusCode != http.StatusBadRequest {
			counterIrisApiClientErrors.Inc(1)
			c.logger.Errorf("irisClient err: %s", clientErr)
		} else {
			c.logger.Warnf("irisClient err: %s", clientErr)
		}
		return nil, clientErr
	}

	return body, nil

}

// PostEscalateIncident set the current step and active state of an iris incident
func (c *IrisClient) PostEscalateIncident(id uint64, step int, active bool) error {

	// if iris dry run is enabled do not escalate incident
	if c.config.IrisDryRun {
		return nil
	}
	path := incidentEscalationPath + strconv.FormatUint(id, 10)
	payload := &IncidentStep{
		Step:   step,
		Active: active,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return err
	}

	_, err = c.Post(path, b)
	if err != nil {
		return err
	}

	return nil
}

// PostFetchIncidents fetch incident details from incidents in incidentIDs
func (c *IrisClient) PostFetchIncidents(incidentIDs []uint64, nodeID string) ([]Incident, error) {

	var incidentList []Incident

	payload := &IncidentIDList{
		IDs: incidentIDs,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	response, err := c.Post(incidentsPath+nodeID, b)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(response, &incidentList); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return incidentList, err
	}

	return incidentList, nil
}

// GetBuildMessage resolve and render a notification into individual messages
func (c *IrisClient) GetBuildMessage(notification Notification) ([]Message, error) {

	b, err := json.Marshal(notification)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	response, err := c.Get(buildMessagePath, nil, b)
	if err != nil {
		return nil, err
	}

	var messageList []Message
	if err := json.Unmarshal(response, &messageList); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	return messageList, nil

}

// GetRenderJinja render jinja from raw template
func (c *IrisClient) GetRenderJinja(templateStr string, context map[string]interface{}) (string, error) {

	// request body sent to iris-api
	type RenderJinjaRequest struct {
		TemplateStr string                 `json:"template_str"`
		Context     map[string]interface{} `json:"context"`
	}

	renderJinjaRequestBody := RenderJinjaRequest{
		TemplateStr: templateStr,
		Context:     context,
	}

	//  iris-api response
	type RenderJinjaResponse struct {
		RenderedBody string `json:"rendered_body"`
	}

	var body RenderJinjaResponse

	b, err := json.Marshal(renderJinjaRequestBody)
	if err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return templateStr, err
	}

	response, err := c.Get(renderJinjaPath, nil, b)
	if err != nil {
		return templateStr, err
	}

	if err := json.Unmarshal(response, &body); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return templateStr, err
	}

	return body.RenderedBody, nil
}

// GetApplicationAuthData fetch a map of iris applications -> api keys
func (c *IrisClient) GetApplicationAuthData() (appKeys map[string]string, err error) {

	appKeys = make(map[string]string)
	response, err := c.Get(authPath, nil, nil)
	if err != nil {
		return nil, err
	}

	var authList []AppAuth
	if err := json.Unmarshal(response, &authList); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}
	for _, app := range authList {
		appKeys[app.Name] = app.Key
	}

	return appKeys, nil
}

// GetApplication fetch application details
func (c *IrisClient) GetApplication(applicationName string) (Application, error) {
	var appResp Application

	response, err := c.Get(applicationDetailsPath+applicationName, nil, nil)
	if err != nil {
		return appResp, err
	}

	if err := json.Unmarshal(response, &appResp); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return appResp, err
	}

	return appResp, nil
}

// GetPlan fetch plan details
func (c *IrisClient) GetPlan(planIdentifier string) (Plan, error) {
	var planResp Plan

	response, err := c.Get(planDetailsPath+planIdentifier, nil, nil)
	if err != nil {
		return planResp, err
	}

	if err := json.Unmarshal(response, &planResp); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return planResp, err
	}

	return planResp, nil
}

// GetTemplate fetch template details
func (c *IrisClient) GetTemplate(templateIdentifier string) (Template, error) {
	var templateResp Template

	response, err := c.Get(templateDetailsPath+templateIdentifier, nil, nil)
	if err != nil {
		return templateResp, err
	}

	if err := json.Unmarshal(response, &templateResp); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return templateResp, err
	}

	return templateResp, nil
}

// GetUser fetch user details
func (c *IrisClient) GetUser(username string) (UserInfo, error) {

	var userResp UserInfo
	response, err := c.Get(userDetailsPath+username, nil, nil)
	if err != nil {
		return userResp, err
	}

	if err := json.Unmarshal(response, &userResp); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return userResp, err
	}

	return userResp, nil
}

// GetInciGetIncidentIDsdents fetch incident ids assigned to this node
func (c *IrisClient) GetIncidentIDs(nodeID string) ([]uint64, error) {

	var incidentIDs []uint64
	response, err := c.Get(incidentsPath+nodeID, nil, nil)
	if err != nil {
		return incidentIDs, err
	}

	if err := json.Unmarshal(response, &incidentIDs); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return incidentIDs, err
	}

	return incidentIDs, nil
}

// GetRoleTarget resolve a role target pairing to map of users
func (c *IrisClient) GetRoleTarget(role string, target string) (map[string]UserInfo, error) {

	var userResp RoleTargetResponse
	var queryParams = map[string][]string{"role": {role}, "target": {target}}
	response, err := c.Get(roleTargetPath, queryParams, nil)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(response, &userResp); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	if len(userResp.Error) > 0 {
		return nil, fmt.Errorf("Iris failed to resolve role:target pairing with error: %s", userResp.Error)
	}

	return userResp.Users, nil
}

// GetPlanAggregationSettings fetches aggregation settings for all active plans
func (c *IrisClient) GetPlanAggregationSettings() ([]PlanAggregationResp, error) {

	var aggSettings []PlanAggregationResp
	response, err := c.Get(planAggregationPath, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(response, &aggSettings); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, err
	}

	return aggSettings, nil
}

// GetPriorityModeMap fetch mode and priority details from iris-api
func (c *IrisClient) GetPriorityModeMap() (map[string]int, map[string]int, map[string]string, error) {

	type PriorityModeMapResponse struct {
		Priorities map[string]int    `json:"priority_map"`
		Modes      map[string]int    `json:"mode_id_map"`
		Defaults   map[string]string `json:"default_priority_map"`
	}
	var responseStruct PriorityModeMapResponse
	response, err := c.Get(priorityModeMapPath, nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	if err := json.Unmarshal(response, &responseStruct); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return nil, nil, nil, err
	}

	return responseStruct.Priorities, responseStruct.Modes, responseStruct.Defaults, nil
}

// GetPeerCount fetch number of message processor peers
func (c *IrisClient) GetPeerCount() (int, error) {

	type peerCountResponse struct {
		PeerCount int `json:"peer_count"`
	}
	var body peerCountResponse

	response, err := c.Get(peerCountPath, nil, nil)
	if err != nil {
		return 1, err
	}

	if err := json.Unmarshal(response, &body); err != nil {
		counterIrisApiClientErrors.Inc(1)
		c.logger.Errorf("irisClient err: %s", err)
		return 1, err
	}

	return body.PeerCount, nil
}

// GetHeartbeat check in with iris-api
func (c *IrisClient) GetHeartbeat(nodeID string) error {

	var queryParams = map[string][]string{"fqdn": {c.config.FQDN}}
	_, err := c.Get(heartbeatPath+nodeID, queryParams, nil)
	return err
}

// Get a http client configured with a timeout that trusts LI certs
func buildHttpClient(TLSCAPath string) (*http.Client, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	if len(TLSCAPath) > 0 {
		caCertBytes, err := ioutil.ReadFile(TLSCAPath)
		if err != nil {
			return nil, fmt.Errorf("Failed grabbing certs from pem %v: %w", TLSCAPath, err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCertBytes) {
			return nil, fmt.Errorf("Failed appending ca cert bytes to pool")
		}

		tlsConfig := &tls.Config{
			RootCAs: certPool,
		}

		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}
	return client, nil
}

// NewIrisClient instantiate new IrisClient
func NewIrisClient(baseurl, application, key string, metricsRegistry *gometrics.Registry, config *Config, logger *Logger) *IrisClient {

	client, err := buildHttpClient(config.TLSCAPath)
	if err != nil {
		logger.Fatalf("Failed to set up HTTP client with error %v", err)
	}

	irisClient := IrisClient{
		baseurl:     baseurl,
		application: application,
		key:         []byte(key),
		http:        client,
		config:      config,
		logger:      logger,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		irisClient.RegisterMetrics(*metricsRegistry)
	}

	return &irisClient
}

/*
 *  Copyright 2021 qitoi
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

package spacedl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/robertkrimen/otto/ast"
	"github.com/robertkrimen/otto/parser"
)

var (
	mainJSRegexp = regexp.MustCompile(`"(https://[^"]*?/main.[a-z0-9]+.js)"`)
	bearerRegexp = regexp.MustCompile(`"(A{10,}[a-zA-Z0-9%]{30,})"`)
)

type Operation struct {
	QueryID       string `json:"query_id"`
	OperationName string `json:"operation_name"`
	OperationType string `json:"operation_type"`
}

type Client struct {
	client      *http.Client
	operations  map[string]*Operation
	bearerToken string
	guestToken  string
}

type QueryParameter struct {
	Name  string
	Value map[string]interface{}
}

type QueryError struct {
	Errors     Errors
	StatusCode int
	Status     string
}

func (q *QueryError) Error() string {
	if len(q.Errors) > 0 {
		return q.Errors[0].Message
	}
	return q.Status
}

type AudioSpaceByIDVariables struct {
	ID                          string `json:"id"`
	IsMetatagsQuery             bool   `json:"isMetatagsQuery"`
	WithSuperFollowsUserFields  bool   `json:"withSuperFollowsUserFields"`
	WithDownvotePerspective     bool   `json:"withDownvotePerspective"`
	WithReactionsMetadata       bool   `json:"withReactionsMetadata"`
	WithReactionsPerspective    bool   `json:"withReactionsPerspective"`
	WithSuperFollowsTweetFields bool   `json:"withSuperFollowsTweetFields"`
	WithReplays                 bool   `json:"withReplays"`
}

type AudioSpaceByIDFeatures struct {
	DontMentionMeViewApiEnabled               bool `json:"dont_mention_me_view_api_enabled"`
	InteractiveTextEnabled                    bool `json:"interactive_text_enabled"`
	ResponsiveWebUcGqlEnabled                 bool `json:"responsive_web_uc_gql_enabled"`
	ResponsiveWebEditTweetApiEnabled          bool `json:"responsive_web_edit_tweet_api_enabled"`
	VibeTweetContextEnabled                   bool `json:"vibe_tweet_context_enabled""`
	StandardizedNudgesForMisinfoNudgesEnabled bool `json:"standardized_nudges_for_misinfo_nudges_enabled"`
	ResponsiveWebEnhanceCardsEnabled          bool `json:"responsive_web_enhance_cards_enabled"`
}

type User struct {
	PeriscopeUserID   string `json:"periscope_user_id"`
	Start             int64  `json:"start"`
	TwitterScreenName string `json:"twitter_screen_name"`
	DisplayName       string `json:"display_name"`
	AvatarUrl         string `json:"avatar_url"`
	IsVerified        bool   `json:"is_verified"`
	IsMutedByAdmin    bool   `json:"is_muted_by_admin"`
	IsMutedByGuest    bool   `json:"is_muted_by_guest"`
	User              struct {
		RestId string `json:"rest_id"`
	} `json:"user"`
}

type Errors []struct {
	Message   string `json:"message"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations"`
	Extensions struct {
		Classification string `json:"classification"`
	} `json:"extensions"`
}

type AudioSpaceByIDResponse struct {
	Data struct {
		AudioSpace struct {
			Metadata struct {
				RestID               string `json:"rest_id"`
				State                string `json:"state"`
				Title                string `json:"title"`
				MediaKey             string `json:"media_key"`
				CreatedAt            int64  `json:"created_at"`
				StartedAt            int64  `json:"started_at"`
				UpdatedAt            int64  `json:"updated_at"`
				IsEmployeeOnly       bool   `json:"is_employee_only"`
				IsLocked             bool   `json:"is_locked"`
				ConversationControls int    `json:"conversation_controls"`
				CreatorResults       struct {
					Result struct {
						Typename string `json:"__typename"`
						ID       string `json:"id"`
						RestId   string `json:"rest_id"`
					} `json:"result"`
				} `json:"creator_results"`
			} `json:"metadata"`
			Participants struct {
				Total     int    `json:"total"`
				Admins    []User `json:"admins"`
				Speakers  []User `json:"speakers"`
				Listeners []User `json:"listeners"`
			} `json:"participants"`
		} `json:"audioSpace"`
	} `json:"data"`
}

type LiveVideoStreamResponse struct {
	Source struct {
		Location              string `json:"location"`
		NoRedirectPlaybackUrl string `json:"noRedirectPlaybackUrl"`
		Status                string `json:"status"`
		StreamType            string `json:"streamType"`
	} `json:"source"`
	SessionId          string `json:"sessionId"`
	ChatToken          string `json:"chatToken"`
	LifecycleToken     string `json:"lifecycleToken"`
	ShareUrl           string `json:"shareUrl"`
	ChatPermissionType string `json:"chatPermissionType"`
}

func GetOwnerUser(resp *AudioSpaceByIDResponse) *User {
	ownerID := resp.Data.AudioSpace.Metadata.CreatorResults.Result.RestId
	for _, u := range resp.Data.AudioSpace.Participants.Admins {
		if u.User.RestId == ownerID {
			return &u
		}
	}
	return nil
}

func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		client: &http.Client{Jar: jar},
	}, nil
}

func (c *Client) Initialize() error {
	jsURL, err := c.getMainJsURL()
	if err != nil {
		return err
	}

	resp, err := c.get(jsURL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	js, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsStr := string(js)
	c.operations = extractOperations(jsStr)
	if len(c.operations) == 0 {
		return errors.New("operations not found")
	}

	c.bearerToken, err = getBearerToken(jsStr)
	if err != nil {
		return err
	}

	c.guestToken, err = getGuestToken(c.bearerToken)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) GetStreamURL(mediaKey string) (string, error) {
	liveVideoStreamURL := fmt.Sprintf("https://twitter.com/i/api/1.1/live_video_stream/status/%s", mediaKey)
	params := make(url.Values)
	params.Add("client", "web")
	params.Add("use_syndication_guest_id", "false")
	params.Add("cookie_set_host", "twitter.com")

	resp, err := c.get(liveVideoStreamURL, &params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var obj LiveVideoStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return "", err
	}

	return obj.Source.Location, nil
}

func (c *Client) get(url string, query *url.Values) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	req.Header.Set("X-Guest-Token", c.guestToken)

	if query != nil {
		req.URL.RawQuery = query.Encode()
	}

	return c.client.Do(req)
}

func (c *Client) getMainJsURL() (string, error) {
	resp, err := c.get("https://twitter.com/", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	index, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := mainJSRegexp.FindSubmatch(index)
	if len(matches) != 2 {
		return "", errors.New("js url not found")
	}

	return string(matches[1]), nil
}

func (c *Client) Query(name string, params []QueryParameter, out interface{}) error {
	op, ok := c.operations[name]
	if !ok {
		return fmt.Errorf("operation not found: %v", name)
	}

	query := make(url.Values)
	for _, v := range params {
		s, err := json.Marshal(v.Value)
		if err != nil {
			return err
		}
		query.Add(v.Name, string(s))
	}

	u := fmt.Sprintf("https://twitter.com/i/api/graphql/%s/%s", op.QueryID, op.OperationName)
	resp, err := c.get(u, &query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return parseResponse(resp, out)
}

func parseResponse(resp *http.Response, out interface{}) error {
	var m map[string]json.RawMessage

	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return err
	}

	var errors Errors
	if raw, ok := m["errors"]; ok {
		if err := json.Unmarshal(raw, &errors); err != nil {
			return err
		}
		delete(m, "errors")
	}

	b, err := json.Marshal(m)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(b, out); err != nil {
		return err
	}

	status := resp.StatusCode / 100
	if len(errors) > 0 || status == 4 || status == 5 {
		return &QueryError{
			Errors:     errors,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}

	return nil
}

func getBearerToken(src string) (string, error) {
	matches := bearerRegexp.FindStringSubmatch(src)
	if len(matches) != 2 {
		return "", errors.New("bearer token not found")
	}
	return matches[1], nil
}

func extractOperations(src string) map[string]*Operation {
	operations := make(map[string]*Operation)

	for {
		idx := strings.Index(src, `operationName:`)
		if idx == -1 {
			break
		}

		s := strings.LastIndexByte(src[:idx], '{')
		nest := 1
		e := s + 1
		for e <= len(src) && nest > 0 {
			switch src[e] {
			case '{':
				nest += 1
			case '}':
				nest -= 1
			}
			e += 1
		}
		obj := "(" + src[s:e] + ")"

		program, err := parser.ParseFile(nil, "main.js", obj, 0)
		if err != nil {
			break
		}

		var op Operation
		for _, b := range program.Body {
			if stmt, ok := b.(*ast.ExpressionStatement); ok {
				if literal, ok := stmt.Expression.(*ast.ObjectLiteral); ok {
					for _, prop := range literal.Value {
						if value, ok := prop.Value.(*ast.StringLiteral); ok {
							switch prop.Key {
							case "queryId":
								op.QueryID = value.Value
							case "operationName":
								op.OperationName = value.Value
							case "operationType":
								op.OperationType = value.Value
							}
						}
					}
				}
			}
		}

		if op.QueryID != "" && op.OperationType != "" && op.OperationName != "" {
			operations[op.OperationName] = &op
		}

		src = src[e:]
	}

	return operations
}

func getGuestToken(bearerToken string) (string, error) {
	req, err := http.NewRequest("post", "https://api.twitter.com/1.1/guest/activate.json", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	type GuestActivateResponse struct {
		GuestToken string `json:"guest_token"`
	}

	var response GuestActivateResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	return response.GuestToken, nil
}

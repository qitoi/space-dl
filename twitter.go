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

const (
	queryErrBadGuestToken = "Bad guest token"
)

var (
	mainJSRegexp    = regexp.MustCompile(`"(https://[^"]*?/main.[a-z0-9]+.js)"`)
	apiSuffixRegexp = regexp.MustCompile(`api:"([a-z0-9]+)"`)
	bearerRegexp    = regexp.MustCompile(`"(A{10,}[a-zA-Z0-9%]{30,})"`)
)

type Operation struct {
	QueryID       string
	OperationName string
	OperationType string
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
	Spaces2022H2Clipping                                           bool `json:"spaces_2022_h2_clipping"`
	Spaces2022H2SpacesCommunities                                  bool `json:"spaces_2022_h2_spaces_communities"`
	ResponsiveWebTwitterBlueVerifiedBadgeIsEnabled                 bool `json:"responsive_web_twitter_blue_verified_badge_is_enabled"`
	VerifiedPhoneLabelEnabled                                      bool `json:"verified_phone_label_enabled"`
	ViewCountsPublicVisibilityEnabled                              bool `json:"view_counts_public_visibility_enabled"`
	LongformNotetweetsConsumptionEnabled                           bool `json:"longform_notetweets_consumption_enabled"`
	TweetypieUnmentionOptimizationEnabled                          bool `json:"tweetypie_unmention_optimization_enabled"`
	ResponsiveWebUcGqlEnabled                                      bool `json:"responsive_web_uc_gql_enabled"`
	VibeApiEnabled                                                 bool `json:"vibe_api_enabled"`
	ResponsiveWebEditTweetApiEnabled                               bool `json:"responsive_web_edit_tweet_api_enabled"`
	GraphqlIsTranslatableRwebTweetIsTranslatableEnabled            bool `json:"graphql_is_translatable_rweb_tweet_is_translatable_enabled"`
	ViewCountsEverywhereApiEnabled                                 bool `json:"view_counts_everywhere_api_enabled"`
	StandardizedNudgesMisinfo                                      bool `json:"standardized_nudges_misinfo"`
	TweetWithVisibilityResultsPreferGqlLimitedActionsPolicyEnabled bool `json:"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled"`
	ResponsiveWebGraphqlTimelineNavigationEnabled                  bool `json:"responsive_web_graphql_timeline_navigation_enabled"`
	InteractiveTextEnabled                                         bool `json:"interactive_text_enabled"`
	ResponsiveWebTextConversationsEnabled                          bool `json:"responsive_web_text_conversations_enabled"`
	ResponsiveWebEnhanceCardsEnabled                               bool `json:"responsive_web_enhance_cards_enabled"`
	ResponsiveWebGraphqlExcludeDirectiveEnabled                    bool `json:"responsive_web_graphql_exclude_directive_enabled"`
	ResponsiveWebGraphqlSkipUserProfileImageExtensionsEnabled      bool `json:"responsive_web_graphql_skip_user_profile_image_extensions_enabled"`
	FreedomOfSpeechNotReachAppealLabelEnabled                      bool `json:"freedom_of_speech_not_reach_appeal_label_enabled"`
}

type User struct {
	PeriscopeUserId   string `json:"periscope_user_id"`
	Start             int64  `json:"start"`
	TwitterScreenName string `json:"twitter_screen_name"`
	DisplayName       string `json:"display_name"`
	AvatarUrl         string `json:"avatar_url"`
	IsVerified        bool   `json:"is_verified"`
	IsMutedByAdmin    bool   `json:"is_muted_by_admin"`
	IsMutedByGuest    bool   `json:"is_muted_by_guest"`
	UserResults       struct {
		RestId string `json:"rest_id"`
		Result struct {
			Typename                              string `json:"__typename"`
			IdentityProfileLabelsHighlightedLabel struct {
			} `json:"identity_profile_labels_highlighted_label"`
			HasNftAvatar   bool `json:"has_nft_avatar"`
			IsBlueVerified bool `json:"is_blue_verified"`
			Legacy         struct {
			} `json:"legacy"`
		} `json:"result"`
	} `json:"user_results"`
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
				RestId                      string `json:"rest_id"`
				State                       string `json:"state"`
				Title                       string `json:"title"`
				MediaKey                    string `json:"media_key"`
				CreatedAt                   int64  `json:"created_at"`
				StartedAt                   int64  `json:"started_at"`
				EndedAt                     string `json:"ended_at"`
				UpdatedAt                   int64  `json:"updated_at"`
				DisallowJoin                bool   `json:"disallow_join"`
				NarrowCastSpaceType         int    `json:"narrow_cast_space_type"`
				IsEmployeeOnly              bool   `json:"is_employee_only"`
				IsLocked                    bool   `json:"is_locked"`
				IsSpaceAvailableForReplay   bool   `json:"is_space_available_for_replay"`
				IsSpaceAvailableForClipping bool   `json:"is_space_available_for_clipping"`
				ConversationControls        int    `json:"conversation_controls"`
				TotalReplayWatched          int    `json:"total_replay_watched"`
				TotalLiveListeners          int    `json:"total_live_listeners"`
				CreatorResults              struct {
					Result struct {
						Typename                   string `json:"__typename"`
						Id                         string `json:"id"`
						RestId                     string `json:"rest_id"`
						AffiliatesHighlightedLabel struct {
						} `json:"affiliates_highlighted_label"`
						HasNftAvatar   bool `json:"has_nft_avatar"`
						IsBlueVerified bool `json:"is_blue_verified"`
						Professional   struct {
							RestId           string        `json:"rest_id"`
							ProfessionalType string        `json:"professional_type"`
							Category         []interface{} `json:"category"`
						} `json:"professional"`
						SuperFollowEligible bool `json:"super_follow_eligible"`
						SuperFollowedBy     bool `json:"super_followed_by"`
						SuperFollowing      bool `json:"super_following"`
					} `json:"result"`
				} `json:"creator_results"`
			} `json:"metadata"`
			Sharings struct {
				Items     []interface{} `json:"items"`
				SliceInfo struct {
				} `json:"slice_info"`
			} `json:"sharings"`
			Participants struct {
				Total     int           `json:"total"`
				Admins    []User        `json:"admins"`
				Speakers  []interface{} `json:"speakers"`
				Listeners []interface{} `json:"listeners"`
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
		if u.UserResults.RestId == ownerID {
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

func replaceURLFile(u string, filename string) (string, error) {
	u2, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	pos := strings.LastIndex(u2.Path, "/")
	u2.Path = u2.Path[:pos+1] + filename
	return u2.String(), nil
}

func (c *Client) Initialize() error {
	index, err := c.getIndex()
	if err != nil {
		return err
	}

	mainJsURL, err := c.getMainJsURL(index)
	if err != nil {
		return err
	}

	fmt.Printf("main js: %v\n", mainJsURL)

	apiJsURL, err := c.getApiJsURL(mainJsURL, index)
	if err != nil {
		return err
	}

	fmt.Printf("api js: %v\n", apiJsURL)

	operations, err := c.getOperations(apiJsURL)
	if err != nil {
		return err
	}
	c.operations = operations

	c.bearerToken, err = c.getBearerToken(mainJsURL)
	if err != nil {
		return err
	}

	if err = c.refreshGuestToken(); err != nil {
		return err
	}

	return nil
}

func (c *Client) getOperations(jsURL string) (map[string]*Operation, error) {
	resp, err := c.get(jsURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	js, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	operations := extractOperations(string(js))
	if len(operations) == 0 {
		return nil, errors.New("operations not found")
	}

	return operations, nil
}

func (c *Client) refreshGuestToken() error {
	token, err := getGuestToken(c.bearerToken)
	if err != nil {
		return err
	}
	c.guestToken = token
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

func (c *Client) getIndex() ([]byte, error) {
	resp, err := c.get("https://twitter.com/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	index, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return index, nil
}

func (c *Client) getMainJsURL(index []byte) (string, error) {
	matches := mainJSRegexp.FindSubmatch(index)
	if len(matches) != 2 {
		return "", errors.New("js url not found")
	}

	return string(matches[1]), nil
}

func (c *Client) getApiJsURL(mainJsUrl string, index []byte) (string, error) {
	apiMatches := apiSuffixRegexp.FindSubmatch(index)
	if len(apiMatches) != 2 {
		return "", errors.New("api suffix not found")
	}

	apiFileName := "api." + string(apiMatches[1]) + "a.js"

	apiJsUrl, err := replaceURLFile(mainJsUrl, apiFileName)
	if err != nil {
		return "", err
	}

	return apiJsUrl, nil
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

	u := fmt.Sprintf("https://api.twitter.com/graphql/%s/%s", op.QueryID, op.OperationName)
	resp, err := c.get(u, &query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = parseResponse(resp, out)
	if qe, ok := err.(*QueryError); ok {
		for _, e := range qe.Errors {
			if strings.EqualFold(e.Message, queryErrBadGuestToken) {
				if err := c.refreshGuestToken(); err != nil {
					return err
				}
				return c.Query(name, params, out)
			}
		}
	}

	return err
}

func parseResponse(resp *http.Response, out interface{}) error {
	var m map[string]json.RawMessage

	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return err
	}

	var errs Errors
	if raw, ok := m["errors"]; ok {
		if err := json.Unmarshal(raw, &errs); err != nil {
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

	if resp.StatusCode == 200 {
		return nil
	}

	status := resp.StatusCode / 100
	if len(errs) > 0 || status == 4 || status == 5 {
		return &QueryError{
			Errors:     errs,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}

	return nil
}

func (c *Client) getBearerToken(jsURL string) (string, error) {
	resp, err := c.get(jsURL, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	js, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := bearerRegexp.FindStringSubmatch(string(js))
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

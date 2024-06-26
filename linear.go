package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func doLinearQuery(ctx context.Context, hc *http.Client, qreq *graphqlQuery, resp interface{}) error {
	b, httpResp, err := doGraphQLQuery(ctx, "https://api.linear.app/graphql", hc, qreq)
	if os.Getenv("DEBUG") != "" {
		if httpResp != nil && httpResp.Header.Get("X-Complexity") != "" {
			log.Printf("linear query with %s complexity", httpResp.Header.Get("X-Complexity"))
		}
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &resp)
}

func queryLinearIssues(ctx context.Context, hc *http.Client, before string) ([]*linearIssue, error) {
	queryString := `query($before: String, $number: Float, $team: String) {
		issues(last: 50, before: $before, filter: {number: {eq: $number}, team: {name: {eq: $team}}}, includeArchived: true) {
			nodes {
				id
				url
				identifier
				title
				description
				creator {
					name
					email
				}
				assignee {
					name
					email
				}
				priorityLabel
				state {
					name
				}
				project {
					name
					description
				}
				createdAt
				comments(last: 10) {
					nodes {
						url
						user {
							name
							email
						}
						createdAt
						body
					}
				}
				attachments(last: 10) {
					nodes {
						url
					}
				}
				relations(last: 10) {
					nodes {
						relatedIssue {
							identifier
						}
					}
				}
				parent {
					identifier
				}
				children(last: 10) {
					nodes {
						identifier
					}
				}
			}
		}
	}`
	var queryResp struct {
		Data struct {
			Issues struct {
				Nodes []*linearIssue `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
	}

	qreq := &graphqlQuery{
		Query:     queryString,
		Variables: map[string]interface{}{},
	}
	if before != "" {
		qreq.Variables["before"] = before
	}
	number, err := strconv.Atoi(byelinearIssueNumber)
	if err == nil {
		qreq.Variables["number"] = number
	}
	qreq.Variables["team"] = byelinearTeamName
	err = doLinearQuery(ctx, hc, qreq, &queryResp)
	if err != nil {
		return nil, err
	}
	return queryResp.Data.Issues.Nodes, nil
}

type linearUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type linearIssue struct {
	ID            string      `json:"id"`
	URL           string      `json:"url"`
	Identifier    string      `json:"identifier"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Creator       *linearUser `json:"creator"`
	Assignee      *linearUser `json:"assignee"`
	PriorityLabel string      `json:"priorityLabel"`
	State         struct {
		Name string `json:"name"`
	} `json:"state"`
	Project struct {
		Name string `json:"name"`
		Desc string `json:"description"`
	} `json:"project"`
	CreatedAt time.Time `json:"createdAt"`
	Comments  struct {
		Nodes []struct {
			URL       string      `json:"url"`
			User      *linearUser `json:"user"`
			CreatedAt time.Time   `json:"createdAt"`
			Body      string      `json:"body"`
		} `json:"nodes"`
	} `json:"comments"`
	Relations struct {
		Nodes []struct {
			RelatedIssue struct {
				Identifier string `json:"identifier"`
			} `json:"relatedIssue"`
		} `json:"nodes"`
	} `json:"relations"`
	Parent struct {
		Identifier string `json:"identifier"`
	} `json:"parent"`
	Children struct {
		Nodes []struct {
			Identifier string `json:"identifier"`
		} `json:"nodes"`
	} `json:"children"`
	Attachments struct {
		Nodes []struct {
			URL string `json:"url"`
		} `json:"nodes"`
	} `json:"attachments"`
}

func (li *linearIssue) relationsArr() []string {
	var a []string
	for _, rel := range li.Relations.Nodes {
		a = append(a, rel.RelatedIssue.Identifier)
	}
	return a
}

func (li *linearIssue) childrenArr() []string {
	var a []string
	for _, ch := range li.Children.Nodes {
		a = append(a, ch.Identifier)
	}
	return a
}

func (li *linearIssue) assignee() string {
	if li.Assignee == nil {
		return ""
	}
	return "@" + emailsToGithubMap[li.Assignee.Email]
}

func (li *linearIssue) attachmentsArr() []string {
	var a []string
	for _, att := range li.Attachments.Nodes {
		a = append(a, att.URL)
	}
	return a
}

func getNumberFromIssue(issue *issueState) int {
	n, err := strconv.Atoi(strings.Split(issue.Identifier, "-")[1])

	if err != nil {
		log.Fatalf("failed to get number from identifier")
	}

	return n
}

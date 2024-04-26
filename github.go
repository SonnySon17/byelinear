package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/go-github/v47/github"
)

func (s *state) exportToGithub(ctx context.Context, gc *github.Client, ident string, iss *githubIssue) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute*2)
	defer cancel()

	issReq := &github.IssueRequest{
		Title:    &iss.title,
		Assignee: &iss.assignee,
		Body:     &iss.body,
	}
	log.Printf("%s: creating", ident)
	giss, res, err := gc.Issues.Create(ctx, orgName, repoName, issReq)
	if err != nil {
		reset := res.Header.Get("x-ratelimit-reset")
		if reset != "" {
			i, _ := strconv.ParseInt(reset, 10, 64)
			t := time.Unix(i, 0)
			log.Printf("Please try after: %v", t)
		}
		return "", err
	}
	if iss.state == "Done" || iss.state == "Canceled" {
		issReq.State = github.String("closed")
		issReq.StateReason = github.String("completed")
		if iss.state == "Canceled" {
			issReq.StateReason = github.String("not_planned")
		}
		_, _, err = gc.Issues.Edit(ctx, orgName, repoName, *giss.Number, issReq)
		if err != nil {
			return "", err
		}
	}
	for i, c := range iss.comments {
		log.Printf("%s: creating comment %d", ident, i)
		_, _, err = gc.Issues.CreateComment(ctx, orgName, repoName, *giss.Number, &github.IssueComment{
			Body: &c,
		})
		if err != nil {
			return "", err
		}
	}

	if byelinearProjectName != "" {
		org, err := queryOrganization(ctx, gc.Client())

		if err != nil {
			return "", err
		}

		var pName = byelinearProjectName
		var pId string
		var pNum int

		for _, p := range org.projects {
			if pName == p.title {
				pId = p.id
				pNum = p.number
			}
		}

		if pId != "" {
			itemId, err := addIssueToProject(ctx, gc.Client(), pId, *giss.NodeID)
			if err != nil {
				return "", err
			}

			sfi, err := queryStatusField(ctx, gc.Client(), pNum)
			if err != nil {
				return "", err
			}

			ps := &projectState{
				Name:            pName,
				ID:              pId,
				StatusFieldInfo: sfi,
			}

			err = setProjectIssueStatus(ctx, gc.Client(), pId, itemId, ps.StatusFieldInfo, iss.state)
			if err != nil {
				return "", err
			}
		}
	}
	return giss.GetHTMLURL(), nil
}

type githubLabel struct {
	name  string
	color string
	desc  string
}

type githubIssue struct {
	title    string
	assignee string
	body     string
	state    string
	project  *githubProject
	comments []string
}

type githubProject struct {
	name string
	desc string
}

func fromLinearIssue(liss *linearIssue) *githubIssue {
	body := fmt.Sprintf(`field | value
| - | - |
url | %s
author | @%s
date | %s
state | %s
project | %s
priority | %s
assignee | %s
related | %s
parent | %s
children | %s
attachments | %s
`,
		liss.URL,
		emailsToGithubMap[liss.Creator.Email],
		formatTime(liss.CreatedAt),
		liss.State.Name,
		liss.Project.Name,
		liss.PriorityLabel,
		liss.assignee(),

		formatArr(liss.relationsArr()),
		liss.Parent.Identifier,
		formatArr(liss.childrenArr()),

		formatArr(liss.attachmentsArr()),
	)
	if liss.Description != "" {
		body += "\n" + liss.Description
	}

	iss := &githubIssue{
		title: liss.Title,
		body:  body,
		state: liss.State.Name,
	}

	for _, c := range liss.Comments.Nodes {
		var email string
		if user := c.User; user != nil {
			email = user.Email
		}
		iss.comments = append(iss.comments, fmt.Sprintf(`field | value
|-|-|
url | %s
author | @%s
date | %s

%s`,
			c.URL,
			emailsToGithubMap[email],
			formatTime(c.CreatedAt),
			c.Body,
		))
	}

	if liss.Project.Name != "" {
		iss.project = &githubProject{
			name: liss.Project.Name,
			desc: liss.Project.Desc,
		}
	}
	if liss.Assignee != nil {
		iss.assignee = emailsToGithubMap[liss.Assignee.Email]
	}
	return iss
}

var emailsToGithubMap = map[string]string{
	"sonny@chai.finance":   "SonnySon17",
	"alex@chai.finance":    "imcheck",
	"chan@portone.io":      "hchanhi",
	"draven@chai.finance":  "wlsgur828",
	"jihyeon@chai.finance": "simnalamburt",
	"leslie@chai.finance":  "lens0021",
	"mae@chai.finance":     "moonjihae",
	"matt@chai.finance":    "khj309",
	"rody@chai.finance":    "aimpugn",
	"wiz@chai.finance":     "smc0210",
	"deedee@chai.finance":  "SoleeChoi",
}

type organization struct {
	id       string
	projects []*project
}

type project struct {
	id     string
	number int
	title  string
	desc   string
}

func queryOrganization(ctx context.Context, hc *http.Client) (*organization, error) {
	queryString := `query($login: String!) {
		organization(login: $login) {
			id
			projectsV2(first: 25) {
				nodes {
					id
					title
					shortDescription
					number
				}
			}
		}
	}`
	var queryResp struct {
		Data struct {
			Organization struct {
				ID         string `json:"id"`
				ProjectsV2 struct {
					Nodes []struct {
						ID     string `json:"id"`
						Title  string `json:"title"`
						Desc   string `json:"shortDescription"`
						Number int    `json:"number"`
					} `json:"nodes"`
				} `json:"projectsv2"`
			} `json:"organization"`
		} `json:"data"`
	}

	qreq := &graphqlQuery{
		Query:     queryString,
		Variables: map[string]interface{}{"login": orgName},
	}
	err := doGithubQuery(ctx, hc, qreq, &queryResp)
	if err != nil {
		return nil, err
	}

	org := &organization{
		id: queryResp.Data.Organization.ID,
	}
	for _, lp := range queryResp.Data.Organization.ProjectsV2.Nodes {
		p := &project{
			id:     lp.ID,
			title:  lp.Title,
			desc:   lp.Desc,
			number: lp.Number,
		}
		org.projects = append(org.projects, p)
	}
	return org, nil
}

type statusFieldInfo struct {
	ID string `json:"ID"`

	TodoID       string `json:"todo_id"`
	InProgressID string `json:"in_progress_id"`
	DoneID       string `json:"done_id"`
}

func queryStatusField(ctx context.Context, hc *http.Client, pnum int) (*statusFieldInfo, error) {
	queryString := `query($login: String!, $projectNumber: Int!) {
		organization(login: $login) {
			projectV2(number: $projectNumber) {
				field(name: "Status") {
					... on ProjectV2SingleSelectField {
						id
						options {
							id
							name
						}
					}
				}
			}
		}
	}`

	var queryResp struct {
		Data struct {
			Organization struct {
				ProjectV2 struct {
					Field struct {
						ID      string `json:"id"`
						Options []struct {
							ID   string `json:"ID"`
							Name string `json:"name"`
						} `json:"options"`
					} `json:"field"`
				} `json:"projectv2"`
			} `json:"organization"`
		} `json:"data"`
	}

	qreq := &graphqlQuery{
		Query:     queryString,
		Variables: map[string]interface{}{"login": orgName, "projectNumber": pnum},
	}
	err := doGithubQuery(ctx, hc, qreq, &queryResp)
	if err != nil {
		return nil, err
	}
	si := &statusFieldInfo{
		ID: queryResp.Data.Organization.ProjectV2.Field.ID,
	}
	for _, o := range queryResp.Data.Organization.ProjectV2.Field.Options {
		switch o.Name {
		case "Todo":
			si.TodoID = o.ID
		case "In Progress":
			si.InProgressID = o.ID
		case "Done":
			si.DoneID = o.ID
		}
	}
	return si, nil
}

func setProjectIssueStatus(ctx context.Context, hc *http.Client, projectID, issID string, si *statusFieldInfo, linearState string) error {
	var optionID string
	switch linearState {
	case "Backlog":
		return nil
	case "Todo":
		optionID = si.TodoID
	case "In Progress", "In Review":
		optionID = si.InProgressID
	case "Done", "Canceled":
		optionID = si.DoneID
	default:
		return nil
	}
	queryString := `mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String) {
		updateProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $fieldId, value: { singleSelectOptionId: $optionId }}) {
			clientMutationId
		}
	}`
	qreq := &graphqlQuery{
		Query:     queryString,
		Variables: map[string]interface{}{"projectId": projectID, "itemId": issID, "fieldId": si.ID, "optionId": optionID},
	}
	return doGithubQuery(ctx, hc, qreq, nil)
}

func createEmptyIssue(ctx context.Context, gc *github.Client) (*string, error) {
	title := "Empty issue"
	githubIssue, res, err := gc.Issues.Create(ctx, orgName, repoName, &github.IssueRequest{Title: &title})
	if err != nil {
		reset := res.Header.Get("x-ratelimit-reset")
		if reset != "" {
			i, _ := strconv.ParseInt(reset, 10, 64)
			t := time.Unix(i, 0)
			log.Printf("Please try after: %v", t)
		}
		return nil, err
	}

	return githubIssue.NodeID, nil
}

func deleteEmptyIssue(ctx context.Context, gc *github.Client, issueId *string) error {
	deleteQuery := `mutation($issueId: ID!) {
		deleteIssue(input: {issueId: $issueId}) {
			clientMutationId
		}
	}`
	request := &graphqlQuery{
		Query:     deleteQuery,
		Variables: map[string]interface{}{"issueId": issueId},
	}

	return doGithubQuery(ctx, gc.Client(), request, nil)
}

func doGithubQuery(ctx context.Context, hc *http.Client, qreq *graphqlQuery, resp interface{}) error {
	b, res, err := doGraphQLQuery(ctx, "https://api.github.com/graphql", hc, qreq)
	if err != nil {
		if res != nil {
			reset := res.Header.Get("x-ratelimit-reset")
			if reset != "" {
				i, _ := strconv.ParseInt(reset, 10, 64)
				t := time.Unix(i, 0)
				log.Printf("Please try after: %v", t)
			}
		}
		return err
	}

	// Github is truly terrible. Rather than set their HTTP status code to a 400 class on
	// validation errors they return an error response like this with the status 200...
	var githubErrors struct {
		Errors []struct {
			Message   string `json:"message"`
			Locations []struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"locations"`
		} `json:"errors"`
	}
	err = json.Unmarshal(b, &githubErrors)
	if err == nil && len(githubErrors.Errors) > 0 {
		return fmt.Errorf("github graphql api error: %v", githubErrors)
	}
	if resp == nil {
		return nil
	}
	return json.Unmarshal(b, &resp)
}

func formatTime(t time.Time) string {
	return t.In(time.Local).Format(time.UnixDate)
}

func addIssueToProject(ctx context.Context, hc *http.Client, pID, iID string) (string, error) {
	queryString := `mutation($projectId: ID!, $contentId: ID!) {
		addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
			item {
				id
			}
		}
	}`

	var queryResp struct {
		Data struct {
			AddProjectV2ItemById struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
	}

	qreq := &graphqlQuery{
		Query:     queryString,
		Variables: map[string]interface{}{"projectId": pID, "contentId": iID},
	}
	err := doGithubQuery(ctx, hc, qreq, &queryResp)
	if err != nil {
		return "", err
	}
	return queryResp.Data.AddProjectV2ItemById.Item.ID, nil
}

func formatArr(v interface{}) string {
	s := fmt.Sprintf("%v", v)
	if s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

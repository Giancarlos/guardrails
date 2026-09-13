package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

// MCP JSON-RPC types
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP types for initialize
type InitializeParams struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ClientCapability `json:"capabilities"`
	ClientInfo      ClientInfo       `json:"clientInfo"`
}

type ClientCapability struct {
	Roots    *RootsCapability `json:"roots,omitempty"`
	Sampling interface{}      `json:"sampling,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ServerCapability `json:"capabilities"`
	ServerInfo      ServerInfo       `json:"serverInfo"`
}

type ServerCapability struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool definitions
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// Tool call types
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP (Model Context Protocol) server",
	Long:  `Run gur as an MCP server for AI assistant integration.`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server over STDIO",
	Long: `Start the gur MCP server using STDIO transport.

This allows AI assistants like Claude to interact with gur through
the Model Context Protocol.

Configure in your MCP client (e.g., claude_desktop_config.json):
{
  "mcpServers": {
    "gur": {
      "command": "/path/to/gur",
      "args": ["mcp", "serve"]
    }
  }
}`,
	RunE: runMCPServe,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpServeCmd)
}

// Define all available tools
func getTools() []Tool {
	return []Tool{
		{
			Name:        "task_list",
			Description: "List tasks with optional filters. Returns task ID, priority, status, title, and type.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"status": {
						Type:        "string",
						Description: "Filter by status",
						Enum:        []string{"open", "in_progress", "closed", "archived"},
					},
					"priority": {
						Type:        "integer",
						Description: "Filter by priority (0=critical, 1=high, 2=medium, 3=low, 4=lowest)",
					},
					"type": {
						Type:        "string",
						Description: "Filter by type",
						Enum:        []string{"task", "bug", "feature", "epic"},
					},
					"assignee": {
						Type:        "string",
						Description: "Filter by assignee",
					},
					"limit": {
						Type:        "integer",
						Description: "Limit number of results",
					},
				},
			},
		},
		{
			Name:        "task_show",
			Description: "Show detailed information about a specific task including dependencies, subtasks, and linked gates.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "string",
						Description: "Task ID (e.g., gur-abc12345)",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "task_create",
			Description: "Create a new task. Returns the created task with its generated ID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title": {
						Type:        "string",
						Description: "Task title (required)",
					},
					"description": {
						Type:        "string",
						Description: "Task description",
					},
					"priority": {
						Type:        "integer",
						Description: "Priority: 0=critical, 1=high, 2=medium (default), 3=low, 4=lowest",
						Default:     2,
					},
					"type": {
						Type:        "string",
						Description: "Task type",
						Enum:        []string{"task", "bug", "feature", "epic"},
						Default:     "task",
					},
					"assignee": {
						Type:        "string",
						Description: "Assignee name",
					},
					"parent": {
						Type:        "string",
						Description: "Parent task ID (creates subtask)",
					},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "task_update",
			Description: "Update an existing task. Only provided fields will be updated.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "string",
						Description: "Task ID to update",
					},
					"title": {
						Type:        "string",
						Description: "New title",
					},
					"description": {
						Type:        "string",
						Description: "New description",
					},
					"priority": {
						Type:        "integer",
						Description: "New priority (0-4)",
					},
					"status": {
						Type:        "string",
						Description: "New status",
						Enum:        []string{"open", "in_progress"},
					},
					"assignee": {
						Type:        "string",
						Description: "New assignee",
					},
					"notes": {
						Type:        "string",
						Description: "Notes to append",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "task_close",
			Description: "Close a task. Requires all linked gates to pass. Use task_gate_pass to verify gates first.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "string",
						Description: "Task ID to close",
					},
					"reason": {
						Type:        "string",
						Description: "Reason for closing (required)",
					},
				},
				Required: []string{"id", "reason"},
			},
		},
		{
			Name:        "task_ready",
			Description: "List tasks that are ready to work on (no open blockers).",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "gate_list",
			Description: "List all quality gates with optional filters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"category": {
						Type:        "string",
						Description: "Filter by category",
					},
					"type": {
						Type:        "string",
						Description: "Filter by type (test, review, approval, manual, etc.)",
					},
				},
			},
		},
		{
			Name:        "gate_show",
			Description: "Show detailed information about a gate including linked tasks and run history.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {
						Type:        "string",
						Description: "Gate ID",
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "gate_create",
			Description: "Create a new quality gate.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title": {
						Type:        "string",
						Description: "Gate title (required)",
					},
					"type": {
						Type:        "string",
						Description: "Gate type: test, review, approval, manual, deploy, qa, security, doc",
						Default:     "manual",
					},
					"category": {
						Type:        "string",
						Description: "Category (e.g., auth, api, ui)",
					},
					"description": {
						Type:        "string",
						Description: "Gate description",
					},
					"command": {
						Type:        "string",
						Description: "Command to run for automated gates",
					},
					"steps": {
						Type:        "string",
						Description: "Steps to verify",
					},
					"expected": {
						Type:        "string",
						Description: "Expected result",
					},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "gate_link",
			Description: "Link a gate to a task. The task cannot be closed until this gate passes.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"gate_id": {
						Type:        "string",
						Description: "Gate ID to link",
					},
					"task_id": {
						Type:        "string",
						Description: "Task ID to link to",
					},
				},
				Required: []string{"gate_id", "task_id"},
			},
		},
		{
			Name:        "gate_pass",
			Description: "Mark a gate as passed for a specific task. Each task requires its own verification.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"gate_id": {
						Type:        "string",
						Description: "Gate ID",
					},
					"task_id": {
						Type:        "string",
						Description: "Task ID",
					},
					"notes": {
						Type:        "string",
						Description: "Notes about the verification",
					},
				},
				Required: []string{"gate_id", "task_id"},
			},
		},
		{
			Name:        "gate_fail",
			Description: "Mark a gate as failed for a specific task.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"gate_id": {
						Type:        "string",
						Description: "Gate ID",
					},
					"task_id": {
						Type:        "string",
						Description: "Task ID",
					},
					"notes": {
						Type:        "string",
						Description: "Notes about the failure",
					},
				},
				Required: []string{"gate_id", "task_id"},
			},
		},
		{
			Name:        "dependency_add",
			Description: "Add a dependency between tasks. The blocker task must be closed before the blocked task can be closed.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"blocker_id": {
						Type:        "string",
						Description: "ID of the blocking task",
					},
					"blocked_id": {
						Type:        "string",
						Description: "ID of the blocked task",
					},
				},
				Required: []string{"blocker_id", "blocked_id"},
			},
		},
		{
			Name:        "search",
			Description: "Search tasks by keyword in title, description, notes, or ID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query": {
						Type:        "string",
						Description: "Search query",
					},
				},
				Required: []string{"query"},
			},
		},
	}
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	// Initialize database for MCP server
	if err := db.EnsureInitialized(); err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			sendError(encoder, nil, -32700, "Parse error", err.Error())
			continue
		}

		response := handleRequest(&req)
		if response != nil {
			encoder.Encode(response)
		}
	}
}

func handleRequest(req *MCPRequest) *MCPResponse {
	switch req.Method {
	case "initialize":
		return handleInitialize(req)
	case "initialized":
		// Notification, no response needed
		return nil
	case "tools/list":
		return handleToolsList(req)
	case "tools/call":
		return handleToolsCall(req)
	case "ping":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}
	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: "Method not found",
				Data:    req.Method,
			},
		}
	}
}

func handleInitialize(req *MCPRequest) *MCPResponse {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapability{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "gur",
			Version: Version,
		},
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func handleToolsList(req *MCPRequest) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": getTools(),
		},
	}
}

func handleToolsCall(req *MCPRequest) *MCPResponse {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	result, err := callTool(params.Name, params.Arguments)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolResult{
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func callTool(name string, args map[string]interface{}) (ToolResult, error) {
	switch name {
	case "task_list":
		return toolTaskList(args)
	case "task_show":
		return toolTaskShow(args)
	case "task_create":
		return toolTaskCreate(args)
	case "task_update":
		return toolTaskUpdate(args)
	case "task_close":
		return toolTaskClose(args)
	case "task_ready":
		return toolTaskReady(args)
	case "gate_list":
		return toolGateList(args)
	case "gate_show":
		return toolGateShow(args)
	case "gate_create":
		return toolGateCreate(args)
	case "gate_link":
		return toolGateLink(args)
	case "gate_pass":
		return toolGatePass(args)
	case "gate_fail":
		return toolGateFail(args)
	case "dependency_add":
		return toolDependencyAdd(args)
	case "search":
		return toolSearch(args)
	default:
		return ToolResult{}, fmt.Errorf("unknown tool: %s", name)
	}
}

// Helper to get string arg
func getStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Helper to get int arg
func getIntArg(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return defaultVal
}

func jsonResult(data interface{}) (ToolResult, error) {
	b, _ := json.MarshalIndent(data, "", "  ")
	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(b)}},
	}, nil
}

func textResult(text string) (ToolResult, error) {
	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}, nil
}

// Tool implementations
func toolTaskList(args map[string]interface{}) (ToolResult, error) {
	database := db.GetDB()
	var tasks []models.Task
	query := database.Order("priority ASC, created_at DESC")

	// Exclude archived by default
	query = query.Where("status != ?", models.StatusArchived)

	if status := getStringArg(args, "status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if priority := getIntArg(args, "priority", -1); priority >= 0 {
		query = query.Where("priority = ?", priority)
	}
	if taskType := getStringArg(args, "type"); taskType != "" {
		query = query.Where("type = ?", taskType)
	}
	if assignee := getStringArg(args, "assignee"); assignee != "" {
		query = query.Where("assignee = ?", assignee)
	}
	if limit := getIntArg(args, "limit", 0); limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&tasks).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{"count": len(tasks), "tasks": tasks})
}

func toolTaskShow(args map[string]interface{}) (ToolResult, error) {
	id := getStringArg(args, "id")
	if id == "" {
		return ToolResult{}, fmt.Errorf("id is required")
	}

	task, err := db.GetTaskByID(id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", id)
	}

	database := db.GetDB()

	// Get dependencies
	var blockedBy, blocks []models.Dependency
	database.Where("child_id = ?", task.ID).Find(&blockedBy)
	database.Where("parent_id = ?", task.ID).Find(&blocks)

	// Get subtasks
	var subtasks []models.Task
	database.Where("parent_id = ?", task.ID).Order("id ASC").Find(&subtasks)

	// Get gate links
	gateLinks, _ := GetGateLinksForTask(task.ID)

	return jsonResult(map[string]interface{}{
		"task":       task,
		"blocked_by": blockedBy,
		"blocks":     blocks,
		"subtasks":   subtasks,
		"gates":      gateLinks,
	})
}

func toolTaskCreate(args map[string]interface{}) (ToolResult, error) {
	title := getStringArg(args, "title")
	if title == "" {
		return ToolResult{}, fmt.Errorf("title is required")
	}

	task := &models.Task{
		Title:       title,
		Description: getStringArg(args, "description"),
		Priority:    getIntArg(args, "priority", models.PriorityMedium),
		Type:        getStringArg(args, "type"),
		Assignee:    getStringArg(args, "assignee"),
		Status:      models.StatusOpen,
	}

	if task.Type == "" {
		task.Type = models.TypeTask
	}

	// Validate priority
	if task.Priority < 0 || task.Priority > 4 {
		return ToolResult{}, fmt.Errorf("priority must be 0-4")
	}

	database := db.GetDB()

	// Handle subtask creation
	if parent := getStringArg(args, "parent"); parent != "" {
		parentTask, err := db.GetTaskByID(parent)
		if err != nil {
			return ToolResult{}, fmt.Errorf("parent task '%s' not found", parent)
		}
		if parentTask.IsClosed() {
			return ToolResult{}, fmt.Errorf("parent task '%s' is closed", parent)
		}
		var count int64
		database.Model(&models.Task{}).Where("parent_id = ?", parent).Count(&count)
		task.ID = models.GenerateSubtaskID(parent, int(count)+1)
		task.ParentID = parent
	}

	if err := database.Create(task).Error; err != nil {
		return ToolResult{}, err
	}

	models.RunHooks(database, models.HookEventOnCreate, task)

	return jsonResult(map[string]interface{}{"success": true, "task": task})
}

func toolTaskUpdate(args map[string]interface{}) (ToolResult, error) {
	id := getStringArg(args, "id")
	if id == "" {
		return ToolResult{}, fmt.Errorf("id is required")
	}

	task, err := db.GetTaskByID(id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", id)
	}

	database := db.GetDB()

	if title := getStringArg(args, "title"); title != "" {
		models.RecordChange(database, task.ID, "title", task.Title, title, "mcp")
		task.Title = title
	}
	if desc := getStringArg(args, "description"); desc != "" {
		models.RecordChange(database, task.ID, "description", task.Description, desc, "mcp")
		task.Description = desc
	}
	if priority := getIntArg(args, "priority", -1); priority >= 0 {
		if priority > 4 {
			return ToolResult{}, fmt.Errorf("priority must be 0-4")
		}
		models.RecordChange(database, task.ID, "priority", fmt.Sprintf("%d", task.Priority), fmt.Sprintf("%d", priority), "mcp")
		task.Priority = priority
	}
	if status := getStringArg(args, "status"); status != "" {
		if status != models.StatusOpen && status != models.StatusInProgress {
			return ToolResult{}, fmt.Errorf("use task_close to close tasks")
		}
		models.RecordChange(database, task.ID, "status", task.Status, status, "mcp")
		task.Status = status
	}
	if assignee := getStringArg(args, "assignee"); assignee != "" {
		models.RecordChange(database, task.ID, "assignee", task.Assignee, assignee, "mcp")
		task.Assignee = assignee
	}
	if notes := getStringArg(args, "notes"); notes != "" {
		models.RecordChange(database, task.ID, "notes", "", notes, "mcp")
		task.AppendNotes(notes)
	}

	if err := database.Save(&task).Error; err != nil {
		return ToolResult{}, err
	}

	models.RunHooks(database, models.HookEventOnUpdate, task)

	return jsonResult(map[string]interface{}{"success": true, "task": task})
}

func toolTaskClose(args map[string]interface{}) (ToolResult, error) {
	id := getStringArg(args, "id")
	reason := getStringArg(args, "reason")

	if id == "" {
		return ToolResult{}, fmt.Errorf("id is required")
	}
	if reason == "" {
		return ToolResult{}, fmt.Errorf("reason is required")
	}

	task, err := db.GetTaskByID(id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", id)
	}

	if task.IsClosed() {
		return ToolResult{}, fmt.Errorf("task '%s' is already closed", id)
	}

	database := db.GetDB()

	// Check for open blockers
	var blockerCount int64
	database.Model(&models.Dependency{}).
		Joins("JOIN tasks ON tasks.id = dependencies.parent_id").
		Where("dependencies.child_id = ? AND dependencies.type = ? AND tasks.status != ?",
			task.ID, models.DepTypeBlocks, models.StatusClosed).
		Count(&blockerCount)

	if blockerCount > 0 {
		return ToolResult{}, fmt.Errorf("task blocked by %d open task(s)", blockerCount)
	}

	// Check for open subtasks
	var openSubtasks int64
	database.Model(&models.Task{}).
		Where("parent_id = ? AND status != ?", task.ID, models.StatusClosed).
		Count(&openSubtasks)

	if openSubtasks > 0 {
		return ToolResult{}, fmt.Errorf("task has %d open subtask(s)", openSubtasks)
	}

	// Check gates (MCP cannot force close)
	if err := CheckGatesBeforeClose(task.ID); err != nil {
		return ToolResult{}, err
	}

	models.RecordChange(database, task.ID, "status", task.Status, models.StatusClosed, "mcp")
	models.RecordChange(database, task.ID, "close_reason", "", reason, "mcp")
	task.Close(reason)

	if err := database.Save(&task).Error; err != nil {
		return ToolResult{}, err
	}

	models.RunHooks(database, models.HookEventOnClose, task)

	return jsonResult(map[string]interface{}{"success": true, "task": task})
}

func toolTaskReady(args map[string]interface{}) (ToolResult, error) {
	database := db.GetDB()

	var blockedTaskIDs []string
	database.Model(&models.Dependency{}).
		Select("DISTINCT dependencies.child_id").
		Joins("JOIN tasks ON tasks.id = dependencies.parent_id").
		Where("dependencies.type = ? AND tasks.status != ?",
			models.DepTypeBlocks, models.StatusClosed).
		Pluck("child_id", &blockedTaskIDs)

	var readyTasks []models.Task
	query := database.Where("status IN ?", []string{models.StatusOpen, models.StatusInProgress})
	if len(blockedTaskIDs) > 0 {
		query = query.Where("id NOT IN ?", blockedTaskIDs)
	}
	if err := query.Order("priority ASC, created_at DESC").Find(&readyTasks).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{"count": len(readyTasks), "tasks": readyTasks})
}

func toolGateList(args map[string]interface{}) (ToolResult, error) {
	database := db.GetDB()
	var gates []models.Gate
	query := database.Order("priority ASC, category ASC, created_at DESC")

	if category := getStringArg(args, "category"); category != "" {
		query = query.Where("category = ?", category)
	}
	if gateType := getStringArg(args, "type"); gateType != "" {
		query = query.Where("type = ?", gateType)
	}

	if err := query.Find(&gates).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{"count": len(gates), "gates": gates})
}

func toolGateShow(args map[string]interface{}) (ToolResult, error) {
	id := getStringArg(args, "id")
	if id == "" {
		return ToolResult{}, fmt.Errorf("id is required")
	}

	gate, err := db.GetGateByID(id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("gate '%s' not found", id)
	}

	database := db.GetDB()

	var links []models.GateTaskLink
	database.Where("gate_id = ?", gate.ID).Find(&links)

	var runs []models.GateRun
	database.Where("gate_id = ?", gate.ID).Order("created_at DESC").Limit(5).Find(&runs)

	return jsonResult(map[string]interface{}{
		"gate":         gate,
		"linked_tasks": links,
		"recent_runs":  runs,
	})
}

func toolGateCreate(args map[string]interface{}) (ToolResult, error) {
	title := getStringArg(args, "title")
	if title == "" {
		return ToolResult{}, fmt.Errorf("title is required")
	}

	gateType := getStringArg(args, "type")
	if gateType == "" {
		gateType = "manual"
	}

	gate := &models.Gate{
		Title:          title,
		Type:           gateType,
		Category:       getStringArg(args, "category"),
		Description:    getStringArg(args, "description"),
		Command:        getStringArg(args, "command"),
		Steps:          getStringArg(args, "steps"),
		ExpectedResult: getStringArg(args, "expected"),
		LastResult:     models.GatePending,
		Priority:       2,
	}

	if err := db.GetDB().Create(gate).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{"success": true, "gate": gate})
}

func toolGateLink(args map[string]interface{}) (ToolResult, error) {
	gateID := getStringArg(args, "gate_id")
	taskID := getStringArg(args, "task_id")

	if gateID == "" || taskID == "" {
		return ToolResult{}, fmt.Errorf("gate_id and task_id are required")
	}

	database := db.GetDB()

	// Validate gate exists
	if _, err := db.GetGateByID(gateID); err != nil {
		return ToolResult{}, fmt.Errorf("gate '%s' not found", gateID)
	}

	// Validate task exists
	if _, err := db.GetTaskByID(taskID); err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", taskID)
	}

	// Check if already linked
	var existing models.GateTaskLink
	if database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&existing).Error == nil {
		return ToolResult{}, fmt.Errorf("gate already linked to task")
	}

	link := &models.GateTaskLink{
		GateID: gateID,
		TaskID: taskID,
		Status: models.GateLinkPending,
	}

	if err := database.Create(link).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{
		"success": true,
		"link":    link,
		"message": fmt.Sprintf("Gate %s linked to task %s. Verify with gate_pass before closing task.", gateID, taskID),
	})
}

func toolGatePass(args map[string]interface{}) (ToolResult, error) {
	gateID := getStringArg(args, "gate_id")
	taskID := getStringArg(args, "task_id")
	notes := getStringArg(args, "notes")

	if gateID == "" || taskID == "" {
		return ToolResult{}, fmt.Errorf("gate_id and task_id are required")
	}

	database := db.GetDB()

	gate, err := db.GetGateByID(gateID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("gate '%s' not found", gateID)
	}

	if _, err := db.GetTaskByID(taskID); err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", taskID)
	}

	var link models.GateTaskLink
	if err := database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&link).Error; err != nil {
		return ToolResult{}, fmt.Errorf("gate not linked to task - use gate_link first")
	}

	// Update link status
	now := time.Now()
	link.Status = models.GateLinkPassed
	link.VerifiedAt = &now
	link.VerifiedBy = "mcp"
	link.Notes = notes
	if err := database.Save(&link).Error; err != nil {
		return ToolResult{}, err
	}

	// Update gate stats
	gate.RecordRun(models.GateLinkPassed, "mcp", notes)
	database.Save(&gate)

	// Record run
	run := &models.GateRun{
		GateID: gateID,
		Result: models.GateLinkPassed,
		RunBy:  "mcp",
		Notes:  notes,
	}
	database.Create(run)

	return jsonResult(map[string]interface{}{
		"success": true,
		"gate":    gate,
		"link":    link,
	})
}

func toolGateFail(args map[string]interface{}) (ToolResult, error) {
	gateID := getStringArg(args, "gate_id")
	taskID := getStringArg(args, "task_id")
	notes := getStringArg(args, "notes")

	if gateID == "" || taskID == "" {
		return ToolResult{}, fmt.Errorf("gate_id and task_id are required")
	}

	database := db.GetDB()

	gate, err := db.GetGateByID(gateID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("gate '%s' not found", gateID)
	}

	if _, err := db.GetTaskByID(taskID); err != nil {
		return ToolResult{}, fmt.Errorf("task '%s' not found", taskID)
	}

	var link models.GateTaskLink
	if err := database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&link).Error; err != nil {
		return ToolResult{}, fmt.Errorf("gate not linked to task - use gate_link first")
	}

	// Update link status
	now := time.Now()
	link.Status = models.GateLinkFailed
	link.VerifiedAt = &now
	link.VerifiedBy = "mcp"
	link.Notes = notes
	if err := database.Save(&link).Error; err != nil {
		return ToolResult{}, err
	}

	// Update gate stats
	gate.RecordRun(models.GateLinkFailed, "mcp", notes)
	database.Save(&gate)

	// Record run
	run := &models.GateRun{
		GateID: gateID,
		Result: models.GateLinkFailed,
		RunBy:  "mcp",
		Notes:  notes,
	}
	database.Create(run)

	return jsonResult(map[string]interface{}{
		"success": true,
		"gate":    gate,
		"link":    link,
	})
}

func toolDependencyAdd(args map[string]interface{}) (ToolResult, error) {
	blockerID := getStringArg(args, "blocker_id")
	blockedID := getStringArg(args, "blocked_id")

	if blockerID == "" || blockedID == "" {
		return ToolResult{}, fmt.Errorf("blocker_id and blocked_id are required")
	}

	database := db.GetDB()

	// Validate both tasks exist
	if _, err := db.GetTaskByID(blockerID); err != nil {
		return ToolResult{}, fmt.Errorf("blocker task '%s' not found", blockerID)
	}
	if _, err := db.GetTaskByID(blockedID); err != nil {
		return ToolResult{}, fmt.Errorf("blocked task '%s' not found", blockedID)
	}

	// Check for circular dependency
	if blockerID == blockedID {
		return ToolResult{}, fmt.Errorf("task cannot block itself")
	}

	// Check if already exists
	var existing models.Dependency
	if database.Where("parent_id = ? AND child_id = ?", blockerID, blockedID).First(&existing).Error == nil {
		return ToolResult{}, fmt.Errorf("dependency already exists")
	}

	dep := &models.Dependency{
		ParentID: blockerID,
		ChildID:  blockedID,
		Type:     models.DepTypeBlocks,
	}

	if err := database.Create(dep).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{
		"success":    true,
		"dependency": dep,
		"message":    fmt.Sprintf("Task %s now blocks %s", blockerID, blockedID),
	})
}

func toolSearch(args map[string]interface{}) (ToolResult, error) {
	query := getStringArg(args, "query")
	if query == "" {
		return ToolResult{}, fmt.Errorf("query is required")
	}

	database := db.GetDB()
	var tasks []models.Task

	searchPattern := "%" + query + "%"
	if err := database.Where(
		"id LIKE ? OR title LIKE ? OR description LIKE ? OR notes LIKE ?",
		searchPattern, searchPattern, searchPattern, searchPattern,
	).Order("priority ASC, created_at DESC").Find(&tasks).Error; err != nil {
		return ToolResult{}, err
	}

	return jsonResult(map[string]interface{}{"count": len(tasks), "query": query, "tasks": tasks})
}

func sendError(encoder *json.Encoder, id interface{}, code int, message, data string) {
	encoder.Encode(&MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func errorResponse(id interface{}, code int, message, data string) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

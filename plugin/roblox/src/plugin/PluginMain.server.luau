--!strict

-- Example Roblox Studio plugin entrypoint for Gox relay-http mode.
-- This script is expected to run as a Plugin script (global `plugin` available).

local PluginBootstrap = require(script.Parent.Parent.bridge.PluginBootstrap)

local pluginRef: Plugin = plugin

local SETTINGS = {
	BaseUrl = "gox_relay_base_url",
	AuthToken = "gox_auth_token",
	ClientName = "gox_client_name",
}

local DEFAULTS = {
	BaseUrl = "http://127.0.0.1:47012",
	AuthToken = "",
	ClientName = "roblox-plugin",
}

local toolbar = pluginRef:CreateToolbar("Gox")
local toggleButton = toolbar:CreateButton(
	"Gox",
	"Open Gox settings",
	"rbxassetid://4458901886"
)

local widgetInfo = DockWidgetPluginGuiInfo.new(
	Enum.InitialDockState.Right,
	false,
	false,
	360,
	250,
	280,
	220
)
local widget = pluginRef:CreateDockWidgetPluginGui("GoxBridgeRelayWidget", widgetInfo)
widget.Title = "Gox"

local root = Instance.new("ScrollingFrame")
root.BackgroundTransparency = 1
root.Size = UDim2.fromScale(1, 1)
root.CanvasSize = UDim2.new(0, 0, 0, 0)
root.AutomaticCanvasSize = Enum.AutomaticSize.Y
root.ScrollBarThickness = 6
root.ScrollingDirection = Enum.ScrollingDirection.Y
root.BorderSizePixel = 0
root.Parent = widget

local list = Instance.new("UIListLayout")
list.Padding = UDim.new(0, 8)
list.HorizontalAlignment = Enum.HorizontalAlignment.Left
list.SortOrder = Enum.SortOrder.LayoutOrder
list.Parent = root

local padding = Instance.new("UIPadding")
padding.PaddingTop = UDim.new(0, 12)
padding.PaddingLeft = UDim.new(0, 12)
padding.PaddingRight = UDim.new(0, 12)
padding.PaddingBottom = UDim.new(0, 12)
padding.Parent = root

local nextOrder = 0
local function nextLayoutOrder(): number
	nextOrder += 1
	return nextOrder
end

local function makeLabel(text: string): TextLabel
	local label = Instance.new("TextLabel")
	label.LayoutOrder = nextLayoutOrder()
	label.BackgroundTransparency = 1
	label.TextXAlignment = Enum.TextXAlignment.Left
	label.Font = Enum.Font.SourceSans
	label.TextSize = 16
	label.Text = text
	label.TextColor3 = Color3.fromRGB(230, 230, 230)
	label.Size = UDim2.new(1, 0, 0, 20)
	label.Parent = root
	return label
end

local function makeTextBox(initialText: string, placeholder: string): TextBox
	local box = Instance.new("TextBox")
	box.LayoutOrder = nextLayoutOrder()
	box.Text = initialText
	box.PlaceholderText = placeholder
	box.ClearTextOnFocus = false
	box.TextXAlignment = Enum.TextXAlignment.Left
	box.Font = Enum.Font.Code
	box.TextSize = 15
	box.TextColor3 = Color3.fromRGB(240, 240, 240)
	box.BackgroundColor3 = Color3.fromRGB(32, 32, 36)
	box.BorderSizePixel = 1
	box.BorderColor3 = Color3.fromRGB(72, 72, 78)
	box.Size = UDim2.new(1, 0, 0, 30)
	box.Parent = root
	return box
end

local function readSetting(key: string, fallback: string): string
	local ok, value = pcall(function()
		return pluginRef:GetSetting(key)
	end)
	if ok and typeof(value) == "string" and string.len(value) > 0 then
		return value
	end
	return fallback
end

local function saveSetting(key: string, value: string)
	pcall(function()
		pluginRef:SetSetting(key, value)
	end)
end

makeLabel("Relay URL")
local baseUrlBox = makeTextBox(readSetting(SETTINGS.BaseUrl, DEFAULTS.BaseUrl), "http://127.0.0.1:47012")

makeLabel("Auth Token")
local authTokenBox = makeTextBox(readSetting(SETTINGS.AuthToken, DEFAULTS.AuthToken), "shared secret token")

makeLabel("Client Name")
local clientNameBox = makeTextBox(readSetting(SETTINGS.ClientName, DEFAULTS.ClientName), "roblox-plugin")

local statusLabel = makeLabel("Status: idle")
statusLabel.TextColor3 = Color3.fromRGB(183, 211, 255)

local buttonRow = Instance.new("Frame")
buttonRow.LayoutOrder = nextLayoutOrder()
buttonRow.BackgroundTransparency = 1
buttonRow.Size = UDim2.new(1, 0, 0, 34)
buttonRow.Parent = root

local rowLayout = Instance.new("UIListLayout")
rowLayout.FillDirection = Enum.FillDirection.Horizontal
rowLayout.Padding = UDim.new(0, 8)
rowLayout.Parent = buttonRow

local startButton = Instance.new("TextButton")
startButton.LayoutOrder = 1
startButton.Text = "Connect"
startButton.Font = Enum.Font.SourceSansSemibold
startButton.TextSize = 16
startButton.TextColor3 = Color3.fromRGB(12, 24, 12)
startButton.BackgroundColor3 = Color3.fromRGB(122, 210, 136)
startButton.BorderSizePixel = 0
startButton.Size = UDim2.new(1, 0, 1, 0)
startButton.Parent = buttonRow

local stopButton = Instance.new("TextButton")
stopButton.LayoutOrder = 2
stopButton.Text = "Disconnect"
stopButton.Font = Enum.Font.SourceSansSemibold
stopButton.TextSize = 16
stopButton.TextColor3 = Color3.fromRGB(236, 236, 236)
stopButton.BackgroundColor3 = Color3.fromRGB(170, 77, 77)
stopButton.BorderSizePixel = 0
stopButton.Size = UDim2.new(1, 0, 1, 0)
stopButton.Parent = buttonRow

local running = false
local activeSession: PluginBootstrap.BridgeSession? = nil

local function updateButtons()
	startButton.Visible = not running
	stopButton.Visible = running
end

updateButtons()

local function persistValues()
	saveSetting(SETTINGS.BaseUrl, baseUrlBox.Text)
	saveSetting(SETTINGS.AuthToken, authTokenBox.Text)
	saveSetting(SETTINGS.ClientName, clientNameBox.Text)
end

local function compactErrorMessage(errValue: any): string
	local text = tostring(errValue)
	text = string.gsub(text, "^.-:%d+:%s*", "")
	text = string.gsub(text, "\n", " | ")
	return text
end

local function trim(text: string): string
	return (string.gsub(text, "^%s*(.-)%s*$", "%1"))
end

startButton.MouseButton1Click:Connect(function()
	local baseUrl = trim(baseUrlBox.Text)
	local authToken = trim(authTokenBox.Text)
	local clientName = trim(clientNameBox.Text)

	if baseUrl == "" then
		statusLabel.Text = "Status: relay URL is required"
		return
	end
	if authToken == "" then
		statusLabel.Text = "Status: auth token is required"
		return
	end

	persistValues()
	running = true
	updateButtons()
	statusLabel.Text = "Status: connecting..."

	local ok, sessionOrErr = pcall(function()
		return PluginBootstrap.StartWithHttpRelayAsync(baseUrl, authToken, clientName)
	end)
	if not ok then
		running = false
		updateButtons()
		local compact = compactErrorMessage(sessionOrErr)
		statusLabel.Text = "Status: error (" .. baseUrl .. "): " .. compact
		warn("[GoxBridge] connect error: " .. tostring(sessionOrErr))
		return
	end
	activeSession = sessionOrErr :: PluginBootstrap.BridgeSession
	statusLabel.Text = "Status: connected"
end)

stopButton.MouseButton1Click:Connect(function()
	if activeSession ~= nil then
		activeSession:Stop()
		activeSession = nil
	end
	running = false
	updateButtons()
	statusLabel.Text = "Status: disconnected"
end)

toggleButton.Click:Connect(function()
	widget.Enabled = not widget.Enabled
end)

pluginRef.Unloading:Connect(function()
	if activeSession ~= nil then
		activeSession:Stop()
		activeSession = nil
	end
	running = false
	updateButtons()
end)

--!strict

local GoxBridge = require(script.Parent.GoxBridge)
local TransportLoop = require(script.Parent.TransportLoop)
local HttpRelayAdapter = require(script.Parent.HttpRelayAdapter)

export type AdapterFactory = () -> TransportLoop.TransportAdapter

local PluginBootstrap = {}

export type BridgeSession = {
	Stop: (self: BridgeSession) -> (),
	IsRunning: (self: BridgeSession) -> boolean,
}

-- Starts the plugin-side bridge loop for a single adapter session.
-- The adapter is expected to provide line-based I/O over a local channel
-- (for example, a TCP relay managed by the plugin host process).
function PluginBootstrap.Start(adapterFactory: AdapterFactory, authToken: string)
	assert(type(authToken) == "string" and string.len(authToken) > 0, "authToken is required")
	local adapter = adapterFactory()
	local bridge = GoxBridge.new()
	local loop = TransportLoop.new(adapter, bridge, authToken)
	loop:Run()
end

function PluginBootstrap.StartAsync(adapterFactory: AdapterFactory, authToken: string): BridgeSession
	assert(type(authToken) == "string" and string.len(authToken) > 0, "authToken is required")
	local adapter = adapterFactory()
	local bridge = GoxBridge.new()
	local loop = TransportLoop.new(adapter, bridge, authToken)
	task.spawn(function()
		local ok, err = pcall(function()
			loop:Run()
		end)
		if not ok then
			warn("[GoxBridge] transport loop crashed: " .. tostring(err))
		else
			warn("[GoxBridge] transport loop exited normally")
		end
	end)
	local session: BridgeSession = {
		Stop = function(_self: BridgeSession)
			loop:Stop()
		end,
		IsRunning = function(_self: BridgeSession)
			return loop:IsRunning()
		end,
	}
	return session
end

function PluginBootstrap.StartWithHttpRelay(baseUrl: string, authToken: string, clientName: string?)
	local adapterFactory = function()
		return HttpRelayAdapter.new(baseUrl, authToken, clientName)
	end
	PluginBootstrap.Start(adapterFactory, authToken)
end

function PluginBootstrap.StartWithHttpRelayAsync(baseUrl: string, authToken: string, clientName: string?): BridgeSession
	local adapterFactory = function()
		return HttpRelayAdapter.new(baseUrl, authToken, clientName)
	end
	return PluginBootstrap.StartAsync(adapterFactory, authToken)
end

return PluginBootstrap

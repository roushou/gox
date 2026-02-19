--!strict

local HttpService = game:GetService("HttpService")
local Protocol = require(script.Parent.Protocol)

type BridgeRequest = Protocol.BridgeRequest
type BridgeResponse = Protocol.BridgeResponse

export type TransportAdapter = {
	ReadLine: (self: TransportAdapter) -> string?,
	WriteLine: (self: TransportAdapter, line: string) -> (),
	Close: (self: TransportAdapter) -> (),
}

type BridgeHandler = {
	HandleRequest: (self: BridgeHandler, request: BridgeRequest) -> BridgeResponse,
}

local TransportLoop = {}
TransportLoop.__index = TransportLoop

local function decodeRequest(encoded: string): (BridgeRequest?, string?)
	local ok, decoded = pcall(function()
		return HttpService:JSONDecode(encoded)
	end)
	if not ok then
		return nil, "invalid JSON payload"
	end
	if typeof(decoded) ~= "table" then
		return nil, "request must be an object"
	end
	local request = decoded :: BridgeRequest
	if request.requestId == nil or request.requestId == "" then
		return nil, "requestId is required"
	end
	if request.correlationId == nil or request.correlationId == "" then
		return nil, "correlationId is required"
	end
	if request.operation == nil or request.operation == "" then
		return nil, "operation is required"
	end
	if request.timestamp == nil or request.timestamp == "" then
		return nil, "timestamp is required"
	end
	if request.idempotencyKey ~= nil and typeof(request.idempotencyKey) ~= "string" then
		return nil, "idempotencyKey must be a string"
	end
	return request, nil
end

function TransportLoop.new(adapter: TransportAdapter, bridge: BridgeHandler, authToken: string)
	assert(adapter ~= nil, "adapter is required")
	assert(bridge ~= nil, "bridge is required")
	assert(authToken ~= nil and authToken ~= "", "auth token is required")

	return setmetatable({
		adapter = adapter,
		bridge = bridge,
		authToken = authToken,
		authenticated = false,
		helloComplete = false,
		running = false,
		stopRequested = false,
		responseCache = {},
		responseCacheOrder = {},
		responseCacheMax = 256,
	}, TransportLoop)
end

function TransportLoop:Run()
	self.running = true
	self.stopRequested = false
	while self.running do
		local line = self.adapter:ReadLine()
		if self.stopRequested then
			break
		end
		if line == nil then
			break
		end

		local request, decodeErr = decodeRequest(line)
		local response: BridgeResponse
		if request == nil then
			local fallbackRequest: BridgeRequest = {
				requestId = "unknown",
				correlationId = "unknown",
				operation = "unknown",
				timestamp = os.date("!%Y-%m-%dT%H:%M:%SZ"),
			}
			response = Protocol.errorResponse(fallbackRequest, "VALIDATION_ERROR", decodeErr or "invalid request", false, nil)
		else
			response = self:_handleRequest(request)
		end

		self.adapter:WriteLine(HttpService:JSONEncode(response))
	end
	self.running = false
	self.adapter:Close()
end

function TransportLoop:Stop()
	self.stopRequested = true
	self.running = false
	self.adapter:Close()
end

function TransportLoop:IsRunning(): boolean
	return self.running
end

function TransportLoop:_handleRequest(request: BridgeRequest): BridgeResponse
	local cacheKey = request.idempotencyKey or request.requestId
	if cacheKey ~= nil then
		local cached = self.responseCache[cacheKey]
		if cached ~= nil then
			return cached
		end
	end

	local response: BridgeResponse
	if not self.authenticated then
		if request.operation ~= "bridge.auth" then
			response = Protocol.errorResponse(request, "ACCESS_DENIED", "authentication required", false, nil)
			self:_cacheResponse(cacheKey, response)
			return response
		end
		local payload = request.payload or {}
		local token = payload.token
		if typeof(token) ~= "string" or token ~= self.authToken then
			response = Protocol.errorResponse(request, "ACCESS_DENIED", "invalid auth token", false, nil)
			self:_cacheResponse(cacheKey, response)
			return response
		end

		self.authenticated = true
		response = Protocol.okResponse(request, {
			authenticated = true,
			sessionId = request.correlationId,
		})
		self:_cacheResponse(cacheKey, response)
		return response
	end

	if request.operation == "bridge.hello" then
		self.helloComplete = true
		response = Protocol.okResponse(request, {
			protocolVersion = "1.1",
			capabilities = {
				"typed-values.v2",
				"instance-id",
				"idempotency-key",
			},
		})
		self:_cacheResponse(cacheKey, response)
		return response
	end

	if not self.helloComplete then
		response = Protocol.errorResponse(request, "ACCESS_DENIED", "bridge.hello required", false, nil)
		self:_cacheResponse(cacheKey, response)
		return response
	end

	if request.operation == "bridge.auth" then
		response = Protocol.okResponse(request, {
			authenticated = true,
		})
		self:_cacheResponse(cacheKey, response)
		return response
	end
	response = self.bridge:HandleRequest(request)
	self:_cacheResponse(cacheKey, response)
	return response
end

function TransportLoop:_cacheResponse(cacheKey: string?, response: BridgeResponse)
	if cacheKey == nil or cacheKey == "" then
		return
	end
	if self.responseCache[cacheKey] == nil then
		table.insert(self.responseCacheOrder, cacheKey)
	end
	self.responseCache[cacheKey] = response
	while #self.responseCacheOrder > self.responseCacheMax do
		local oldest = table.remove(self.responseCacheOrder, 1)
		if oldest ~= nil then
			self.responseCache[oldest] = nil
		end
	end
end

return TransportLoop

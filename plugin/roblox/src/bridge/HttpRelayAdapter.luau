--!strict

local HttpService = game:GetService("HttpService")

local HttpRelayAdapter = {}
HttpRelayAdapter.__index = HttpRelayAdapter

type HttpRelayAdapterType = {
	baseUrl: string,
	authToken: string,
	clientName: string,
	sessionId: string?,
	closed: boolean,
	ReadLine: (self: HttpRelayAdapterType) -> string?,
	WriteLine: (self: HttpRelayAdapterType, line: string) -> (),
	Close: (self: HttpRelayAdapterType) -> (),
}

local function trim(text: string): string
	return (string.gsub(text, "^%s*(.-)%s*$", "%1"))
end

local function normalizeBaseUrl(baseUrl: string): string
	local normalized = trim(baseUrl)
	normalized = string.gsub(normalized, "/+$", "")
	normalized = string.gsub(normalized, "://localhost", "://127.0.0.1", 1)
	return normalized
end

local function requestJson(method: string, url: string, authToken: string, body: any?): (boolean, any, number)
	local headers = {
		["Content-Type"] = "application/json",
		["X-Gox-Auth"] = authToken,
	}
	local requestOptions: { [string]: any } = {
		Url = url,
		Method = method,
		Headers = headers,
	}
	if body ~= nil then
		requestOptions.Body = HttpService:JSONEncode(body)
	end
	local ok, response = pcall(function()
		return HttpService:RequestAsync(requestOptions)
	end)
	if not ok then
		return false, tostring(response), 0
	end
	local statusCode = tonumber((response :: any).StatusCode) or 0
	if not (response :: any).Success then
		return false, "http " .. tostring(statusCode), statusCode
	end
	local bodyText = (response :: any).Body
	if bodyText == nil or bodyText == "" then
		return true, nil, statusCode
	end
	local decodeOk, decoded = pcall(function()
		return HttpService:JSONDecode(bodyText)
	end)
	if not decodeOk then
		return false, "invalid json response", statusCode
	end
	return true, decoded, statusCode
end

function HttpRelayAdapter.new(baseUrl: string, authToken: string, clientName: string?): HttpRelayAdapterType
	local normalizedBaseUrl = normalizeBaseUrl(baseUrl)
	assert(normalizedBaseUrl ~= nil and normalizedBaseUrl ~= "", "baseUrl is required")
	assert(authToken ~= nil and authToken ~= "", "authToken is required")
	local self = setmetatable({
		baseUrl = normalizedBaseUrl,
		authToken = authToken,
		clientName = clientName or "roblox-plugin",
		sessionId = nil,
		closed = false,
	}, HttpRelayAdapter)
	self:_openSession()
	return self
end

function HttpRelayAdapter:_openSession()
	local ok, payload = requestJson(
		"POST",
		self.baseUrl .. "/v1/bridge/session",
		self.authToken,
		{ clientName = self.clientName }
	)
	assert(ok, tostring(payload))
	assert(payload ~= nil and payload.sessionId ~= nil and payload.sessionId ~= "", "missing sessionId")
	self.sessionId = payload.sessionId
end

function HttpRelayAdapter:ReadLine(): string?
	if self.closed then
		return nil
	end
	if self.sessionId == nil then
		self:_openSession()
	end

	local url = self.baseUrl
		.. "/v1/bridge/session/"
		.. (self.sessionId :: string)
		.. "/read?timeout_ms=3000"

	local backoff = 0.1
	while not self.closed do
		local ok, payload, statusCode = requestJson("GET", url, self.authToken, nil)
		if not ok then
			-- Session likely expired/reset; reopen and retry.
			if statusCode == 401 or statusCode == 404 then
				self.sessionId = nil
				local reopenOk, reopenErr = pcall(function()
					self:_openSession()
				end)
				if not reopenOk then
					warn("[GoxBridge] failed to reopen session: " .. tostring(reopenErr))
					task.wait(backoff)
					backoff = math.min(backoff * 2, 2)
					continue
				end
				url = self.baseUrl .. "/v1/bridge/session/" .. (self.sessionId :: string) .. "/read?timeout_ms=3000"
				backoff = 0.1
				continue
			end
			if statusCode == 204 then
				continue
			end
			warn("[GoxBridge] ReadLine request failed: " .. tostring(payload))
			task.wait(backoff)
			backoff = math.min(backoff * 2, 2)
			continue
		end
		if payload ~= nil and payload.line ~= nil then
			backoff = 0.1
			return payload.line
		end
		-- No data yet (204 / empty response), poll again
	end
	return nil
end

function HttpRelayAdapter:WriteLine(line: string)
	if self.closed then
		return
	end
	assert(line ~= nil and line ~= "", "line is required")
	if self.sessionId == nil then
		self:_openSession()
	end
	local url = self.baseUrl .. "/v1/bridge/session/" .. (self.sessionId :: string) .. "/write"
	local ok, _, statusCode = requestJson("POST", url, self.authToken, { line = line })
	if not ok and (statusCode == 401 or statusCode == 404) then
		self.sessionId = nil
		self:_openSession()
		url = self.baseUrl .. "/v1/bridge/session/" .. (self.sessionId :: string) .. "/write"
		ok, _, _ = requestJson("POST", url, self.authToken, { line = line })
	end
	if not ok then
		error("failed to write relay line")
	end
end

function HttpRelayAdapter:Close()
	if self.sessionId ~= nil then
		local closeUrl = self.baseUrl .. "/v1/bridge/session/" .. (self.sessionId :: string) .. "/close"
		requestJson("DELETE", closeUrl, self.authToken, nil)
	end
	self.closed = true
end

return HttpRelayAdapter

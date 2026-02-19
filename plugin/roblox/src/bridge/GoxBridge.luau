--!strict

local HttpService = game:GetService("HttpService")
local Protocol = require(script.Parent.Protocol)

type BridgeRequest = Protocol.BridgeRequest
type BridgeResponse = Protocol.BridgeResponse

local GoxBridge = {}
GoxBridge.__index = GoxBridge

local scriptTypeToClassName: { [string]: string } = {
	Script = "Script",
	LocalScript = "LocalScript",
	ModuleScript = "ModuleScript",
}

local function asString(value: any): (string?, string?)
	if typeof(value) ~= "string" then
		return nil, "must be a string"
	end
	local v = (value :: string)
	if string.len(v) == 0 then
		return nil, "cannot be empty"
	end
	return v, nil
end

local function resolvePath(path: string): Instance?
	local segments = string.split(path, ".")
	local current: Instance = game

	-- Strip leading "game" prefix (e.g. "game.ServerScriptService" → "ServerScriptService")
	if #segments > 0 and segments[1] == "game" then
		table.remove(segments, 1)
	end
	if #segments == 0 then
		return game
	end

	for i, segment in ipairs(segments) do
		if segment == "" then
			return nil
		end
		if i == 1 then
			local ok, service = pcall(function()
				return game:GetService(segment)
			end)
			if ok and service ~= nil then
				current = service :: Instance
				continue
			end
		end
		local nextNode = current:FindFirstChild(segment)
		if nextNode == nil then
			return nil
		end
		current = nextNode
	end

	return current
end

local function buildPath(instance: Instance): string
	local parts: { string } = {}
	local cursor: Instance? = instance
	while cursor ~= nil and cursor ~= game do
		table.insert(parts, 1, cursor.Name)
		cursor = cursor.Parent
	end
	return table.concat(parts, ".")
end

local instanceIdToRef: { [string]: Instance } = {}
local instanceRefToId = setmetatable({}, { __mode = "k" })
local instanceIdSeq = 0

local function ensureInstanceId(instance: Instance): string
	local existing = instanceRefToId[instance]
	if existing ~= nil then
		return existing
	end
	instanceIdSeq += 1
	local newID = "inst-" .. tostring(instanceIdSeq)
	instanceRefToId[instance] = newID
	instanceIdToRef[newID] = instance
	return newID
end

local function resolveByInstanceID(instanceId: any): Instance?
	if typeof(instanceId) ~= "string" or instanceId == "" then
		return nil
	end
	local candidate = instanceIdToRef[instanceId]
	if candidate == nil then
		return nil
	end
	-- Destroyed instances are no longer anchored in the DataModel.
	if candidate.Parent == nil and candidate ~= game then
		instanceIdToRef[instanceId] = nil
		instanceRefToId[candidate] = nil
		return nil
	end
	return candidate
end

local function resolveTarget(payload: { [string]: any }, pathKey: string, idKey: string): (Instance?, string?)
	local byId = resolveByInstanceID(payload[idKey])
	if byId ~= nil then
		return byId, nil
	end
	local rawPath = payload[pathKey]
	if rawPath == nil then
		if payload[idKey] ~= nil then
			return nil, idKey .. " not found"
		end
		return nil, pathKey .. " is required"
	end
	if typeof(rawPath) ~= "string" or rawPath == "" then
		return nil, pathKey .. " must be a non-empty string"
	end
	local byPath = resolvePath(rawPath)
	if byPath == nil then
		return nil, pathKey .. " not found"
	end
	return byPath, nil
end

local function serializeInstance(instance: Instance): { [string]: any }
	local parentPath = ""
	if instance.Parent ~= nil and instance.Parent ~= game then
		parentPath = buildPath(instance.Parent)
	end

	return {
		instanceId = ensureInstanceId(instance),
		name = instance.Name,
		className = instance.ClassName,
		path = buildPath(instance),
		parentPath = parentPath,
		childCount = #instance:GetChildren(),
	}
end

local function optionalNonEmptyString(payload: { [string]: any }, key: string): (string?, string?)
	local raw = payload[key]
	if raw == nil then
		return nil, nil
	end
	local value, err = asString(raw)
	if value == nil then
		return nil, key .. " " .. (err :: string)
	end
	return value, nil
end

local function parseLimit(payload: { [string]: any }, defaultLimit: number): (number, string?)
	local hasLimit = payload.limit ~= nil
	local hasLegacyMaxResults = payload.maxResults ~= nil
	if hasLimit and hasLegacyMaxResults then
		return 0, "only one of limit or maxResults may be provided"
	end

	local raw = payload.limit
	if raw == nil then
		raw = payload.maxResults
	end
	if raw == nil then
		return defaultLimit, nil
	end
	if typeof(raw) ~= "number" then
		return 0, "limit must be a number"
	end
	local limit = math.floor(raw :: number)
	if limit < 1 then
		return 0, "limit must be >= 1"
	end
	if limit > 500 then
		limit = 500
	end
	return limit, nil
end

local function parseOffset(payload: { [string]: any }): (number, string?)
	local raw = payload.offset
	if raw == nil then
		return 0, nil
	end
	if typeof(raw) ~= "number" then
		return 0, "offset must be a number"
	end
	local offset = math.floor(raw :: number)
	if offset < 0 then
		return 0, "offset must be >= 0"
	end
	return offset, nil
end

local function validateRequestShape(decoded: any): (BridgeRequest?, string?)
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
	return request, nil
end

function GoxBridge.new()
	return setmetatable({}, GoxBridge)
end

function GoxBridge:HandleJson(encodedRequest: string): string
	local ok, decoded = pcall(function()
		return HttpService:JSONDecode(encodedRequest)
	end)
	if not ok then
		local fallbackRequest: BridgeRequest = {
			requestId = "unknown",
			correlationId = "unknown",
			operation = "unknown",
			timestamp = os.date("!%Y-%m-%dT%H:%M:%SZ"),
		}
		local response = Protocol.errorResponse(
			fallbackRequest,
			"VALIDATION_ERROR",
			"invalid JSON payload",
			false,
			nil
		)
		return HttpService:JSONEncode(response)
	end

	local request, requestErr = validateRequestShape(decoded)
	if request == nil then
		local fallbackRequest: BridgeRequest = {
			requestId = "unknown",
			correlationId = "unknown",
			operation = "unknown",
			timestamp = os.date("!%Y-%m-%dT%H:%M:%SZ"),
		}
		local response = Protocol.errorResponse(
			fallbackRequest,
			"VALIDATION_ERROR",
			requestErr or "invalid request",
			false,
			nil
		)
		return HttpService:JSONEncode(response)
	end

	local response = self:HandleRequest(request)
	return HttpService:JSONEncode(response)
end

function GoxBridge:HandleRequest(request: BridgeRequest): BridgeResponse
	return self:_handle(request)
end

function GoxBridge:_handle(request: BridgeRequest): BridgeResponse
	if request.operation == "bridge.ping" then
		return Protocol.okResponse(request, {
			ok = true,
			placeId = game.PlaceId,
			studio = true,
		})
	end

	if request.operation == "script.create" then
		return self:_handleScriptCreate(request)
	end

	if request.operation == "script.update" then
		return self:_handleScriptUpdate(request)
	end

	if request.operation == "script.delete" then
		return self:_handleScriptDelete(request)
	end

	if request.operation == "script.get_source" then
		return self:_handleScriptGetSource(request)
	end

	if request.operation == "script.execute" then
		return self:_handleScriptExecute(request)
	end

	if request.operation == "instance.create" then
		return self:_handleInstanceCreate(request)
	end

	if request.operation == "instance.set_property" then
		return self:_handleInstanceSetProperty(request)
	end

	if request.operation == "instance.delete" then
		return self:_handleInstanceDelete(request)
	end

	if request.operation == "instance.get" then
		return self:_handleInstanceGet(request)
	end

	if request.operation == "instance.list_children" then
		return self:_handleInstanceListChildren(request)
	end

	if request.operation == "instance.find" then
		return self:_handleInstanceFind(request)
	end

	return Protocol.errorResponse(
		request,
		"NOT_FOUND",
		("operation %q is not supported"):format(request.operation),
		false,
		nil
	)
end

function GoxBridge:_handleScriptCreate(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local parentPath = payload.parentPath
	local name, nameErr = asString(payload.name)
	if name == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "name " .. (nameErr :: string), false, nil)
	end
	local scriptType, scriptTypeErr = asString(payload.scriptType)
	if scriptType == nil then
		return Protocol.errorResponse(
			request,
			"VALIDATION_ERROR",
			"scriptType " .. (scriptTypeErr :: string),
			false,
			nil
		)
	end
	local source, sourceErr = asString(payload.source)
	if source == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "source " .. (sourceErr :: string), false, nil)
	end

	local className = scriptTypeToClassName[scriptType]
	if className == nil then
		return Protocol.errorResponse(
			request,
			"VALIDATION_ERROR",
			"scriptType must be Script, LocalScript or ModuleScript",
			false,
			nil
		)
	end

	local parent, parentErr = resolveTarget(payload, "parentPath", "parentId")
	if parent == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", parentErr :: string, false, {
			parentPath = parentPath,
			parentId = payload.parentId,
		})
	end

	local dryRun = payload.dryRun == true
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("create %s %q under %s"):format(className, name, parentPath),
		})
	end

	local scriptInstance = Instance.new(className)
	scriptInstance.Name = name
	local sourceOk, sourceSetErr = pcall(function()
		(scriptInstance :: any).Source = source
	end)
	if not sourceOk then
		scriptInstance:Destroy()
		return Protocol.errorResponse(request, "INTERNAL_ERROR", "failed to set script source", false, {
			error = tostring(sourceSetErr),
		})
	end

	scriptInstance.Parent = parent
	return Protocol.okResponse(request, {
		created = true,
		className = className,
		path = buildPath(scriptInstance),
		instanceId = ensureInstanceId(scriptInstance),
		diffSummary = ("created %s %q"):format(className, name),
	})
end

function GoxBridge:_handleScriptUpdate(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local scriptPath = payload.scriptPath
	local source, sourceErr = asString(payload.source)
	if source == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "source " .. (sourceErr :: string), false, nil)
	end

	local target, targetErr = resolveTarget(payload, "scriptPath", "scriptId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			scriptPath = scriptPath,
			scriptId = payload.scriptId,
		})
	end
	if not target:IsA("LuaSourceContainer") then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "target is not a script", false, {
			scriptPath = scriptPath,
		})
	end

	local dryRun = payload.dryRun == true
	local resolvedPath = buildPath(target)
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("update %s source"):format(resolvedPath),
		})
	end

	local ok, setErr = pcall(function()
		(target :: any).Source = source
	end)
	if not ok then
		return Protocol.errorResponse(request, "INTERNAL_ERROR", "failed to update script source", false, {
			error = tostring(setErr),
		})
	end
	return Protocol.okResponse(request, {
		updated = true,
		path = resolvedPath,
		instanceId = ensureInstanceId(target),
		diffSummary = ("updated %s source"):format(resolvedPath),
	})
end

function GoxBridge:_handleScriptDelete(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local scriptPath = payload.scriptPath

	local target, targetErr = resolveTarget(payload, "scriptPath", "scriptId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			scriptPath = scriptPath,
			scriptId = payload.scriptId,
		})
	end
	if not target:IsA("LuaSourceContainer") then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "target is not a script", false, {
			scriptPath = scriptPath,
		})
	end

	local dryRun = payload.dryRun == true
	local resolvedPath = buildPath(target)
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("delete %s"):format(resolvedPath),
		})
	end

	target:Destroy()
	return Protocol.okResponse(request, {
		deleted = true,
		path = resolvedPath,
		diffSummary = ("deleted %s"):format(resolvedPath),
	})
end

function GoxBridge:_handleScriptGetSource(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local scriptPath = payload.scriptPath

	local target, targetErr = resolveTarget(payload, "scriptPath", "scriptId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			scriptPath = scriptPath,
			scriptId = payload.scriptId,
		})
	end
	if not target:IsA("LuaSourceContainer") then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "target is not a script", false, {
			scriptPath = scriptPath,
		})
	end

	local ok, sourceOrErr = pcall(function()
		return (target :: any).Source
	end)
	if not ok then
		return Protocol.errorResponse(request, "INTERNAL_ERROR", "failed to read script source", false, {
			scriptPath = scriptPath,
			error = tostring(sourceOrErr),
		})
	end

	return Protocol.okResponse(request, {
		path = buildPath(target),
		instanceId = ensureInstanceId(target),
		className = target.ClassName,
		source = sourceOrErr,
	})
end

local function normalizeReturnValue(value: any, depth: number?): any
	local maxDepth = depth or 0
	if maxDepth > 5 then
		return "<max-depth>"
	end
	local t = typeof(value)
	if t == "nil" or t == "boolean" or t == "number" or t == "string" then
		return value
	end
	if t == "Instance" then
		return serializeInstance(value :: Instance)
	end
	if t == "table" then
		local raw = value :: any
		local isArray = true
		local count = 0
		for k, _ in pairs(raw) do
			count += 1
			if typeof(k) ~= "number" then
				isArray = false
				break
			end
		end
		if isArray then
			local out = {}
			for i, item in ipairs(raw) do
				out[i] = normalizeReturnValue(item, maxDepth + 1)
			end
			return out
		end
		local out: { [string]: any } = {}
		for k, v in pairs(raw) do
			out[tostring(k)] = normalizeReturnValue(v, maxDepth + 1)
			if count > 128 then
				out["__truncated"] = true
				break
			end
		end
		return out
	end
	return tostring(value)
end

local function resolveExecutableScript(payload: { [string]: any }): (LuaSourceContainer?, string?, string?)
	local target, targetErr = resolveTarget(payload, "scriptPath", "scriptId")
	if target == nil then
		return nil, nil, targetErr
	end
	if not target:IsA("LuaSourceContainer") then
		return nil, nil, "target is not a script"
	end
	return target :: LuaSourceContainer, buildPath(target), nil
end

function GoxBridge:_handleScriptExecute(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local target, resolvedPath, targetErr = resolveExecutableScript(payload)
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr or "script not found", false, nil)
	end

	local functionName, functionNameErr = optionalNonEmptyString(payload, "functionName")
	if functionNameErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", functionNameErr :: string, false, nil)
	end
	local args = payload.args
	if args ~= nil and typeof(args) ~= "table" then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "args must be an array", false, nil)
	end
	local dryRun = payload.dryRun == true
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			path = resolvedPath,
			instanceId = ensureInstanceId(target :: Instance),
			diffSummary = ("execute %s"):format(resolvedPath),
		})
	end

	local moduleToRequire: ModuleScript
	local tempModule: ModuleScript? = nil
	if target:IsA("ModuleScript") and payload.forceRequire ~= true then
		moduleToRequire = target :: ModuleScript
	else
		tempModule = Instance.new("ModuleScript")
		tempModule.Name = "__gox_exec_" .. (target :: Instance).Name
		local sourceOk, sourceOrErr = pcall(function()
			return (target :: any).Source
		end)
		if not sourceOk then
			tempModule:Destroy()
			return Protocol.errorResponse(request, "INTERNAL_ERROR", "failed to read script source", false, {
				error = tostring(sourceOrErr),
			})
		end
		(tempModule :: any).Source = sourceOrErr
		moduleToRequire = tempModule
	end

	local requiredValue: any = nil
	local requireOk, requireErr = pcall(function()
		requiredValue = require(moduleToRequire)
	end)
	if tempModule ~= nil then
		tempModule:Destroy()
	end
	if not requireOk then
		return Protocol.errorResponse(request, "INTERNAL_ERROR", "script execution failed", true, {
			error = tostring(requireErr),
			path = resolvedPath,
		})
	end

	local called = false
	local returnValue = requiredValue
	if functionName ~= nil then
		local fn: any = nil
		if typeof(requiredValue) == "table" then
			fn = (requiredValue :: any)[functionName]
		end
		if typeof(fn) ~= "function" then
			return Protocol.errorResponse(request, "NOT_FOUND", ("function %q not found on module export"):format(functionName), false, {
				path = resolvedPath,
			})
		end
		local invokeOk, invokeErr = pcall(function()
			called = true
			if args ~= nil then
				returnValue = fn(table.unpack(args :: any))
			else
				returnValue = fn()
			end
		end)
		if not invokeOk then
			return Protocol.errorResponse(request, "INTERNAL_ERROR", "script function invocation failed", true, {
				error = tostring(invokeErr),
				functionName = functionName,
				path = resolvedPath,
			})
		end
	end

	if payload.expectReturn == true and returnValue == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "script execution returned nil while expectReturn=true", false, {
			path = resolvedPath,
		})
	end

	return Protocol.okResponse(request, {
		executed = true,
		called = called,
		path = resolvedPath,
		instanceId = ensureInstanceId(target :: Instance),
		returnValue = normalizeReturnValue(returnValue, 0),
		diffSummary = ("executed %s"):format(resolvedPath),
	})
end

function GoxBridge:_handleInstanceCreate(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local parentPath = payload.parentPath
	local className, classErr = asString(payload.className)
	if className == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "className " .. (classErr :: string), false, nil)
	end
	local name, nameErr = asString(payload.name)
	if name == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "name " .. (nameErr :: string), false, nil)
	end

	local parent, parentErr = resolveTarget(payload, "parentPath", "parentId")
	if parent == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", parentErr :: string, false, {
			parentPath = parentPath,
			parentId = payload.parentId,
		})
	end

	local dryRun = payload.dryRun == true
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("create %s %q under %s"):format(className, name, parentPath),
		})
	end

	local ok, instanceOrErr = pcall(function()
		return Instance.new(className)
	end)
	if not ok then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "invalid className", false, {
			className = className,
			error = tostring(instanceOrErr),
		})
	end
	local created = instanceOrErr :: Instance
	created.Name = name
	created.Parent = parent
	return Protocol.okResponse(request, {
		created = true,
		className = className,
		path = buildPath(created),
		instanceId = ensureInstanceId(created),
		diffSummary = ("created %s %q"):format(className, name),
	})
end

local function coerceValue(value: any): (any, string?)
	if typeof(value) ~= "table" then
		return value, nil
	end
	local tag = value._type
	if tag == nil then
		return value, nil
	end

	if tag == "Vector3" then
		return Vector3.new(value.x or 0, value.y or 0, value.z or 0), nil
	elseif tag == "Vector2" then
		return Vector2.new(value.x or 0, value.y or 0), nil
	elseif tag == "Ray" then
		local origin = Vector3.new(value.ox or 0, value.oy or 0, value.oz or 0)
		local direction = Vector3.new(value.dx or 0, value.dy or 0, value.dz or 0)
		return Ray.new(origin, direction), nil
	elseif tag == "Color3" then
		return Color3.fromRGB(value.r or 0, value.g or 0, value.b or 0), nil
	elseif tag == "Color3Float" then
		return Color3.new(value.r or 0, value.g or 0, value.b or 0), nil
	elseif tag == "CFrame" then
		if typeof(value.components) == "table" and #value.components == 12 then
			return CFrame.new(
				value.components[1],
				value.components[2],
				value.components[3],
				value.components[4],
				value.components[5],
				value.components[6],
				value.components[7],
				value.components[8],
				value.components[9],
				value.components[10],
				value.components[11],
				value.components[12]
			), nil
		end
		return CFrame.new(value.x or 0, value.y or 0, value.z or 0), nil
	elseif tag == "UDim2" then
		return UDim2.new(value.sx or 0, value.ox or 0, value.sy or 0, value.oy or 0), nil
	elseif tag == "UDim" then
		return UDim.new(value.scale or 0, value.offset or 0), nil
	elseif tag == "RectFloat" then
		return Rect.new(
			value.minX or 0,
			value.minY or 0,
			value.maxX or 0,
			value.maxY or 0
		), nil
	elseif tag == "BrickColor" then
		if typeof(value.name) ~= "string" or value.name == "" then
			return nil, "BrickColor requires a non-empty 'name' field"
		end
		return BrickColor.new(value.name), nil
	elseif tag == "Enum" then
		if typeof(value.enum) ~= "string" or value.enum == "" then
			return nil, "Enum requires a non-empty 'enum' field"
		end
		if typeof(value.value) ~= "string" or value.value == "" then
			return nil, "Enum requires a non-empty 'value' field"
		end
		local enumGroup = (Enum :: any)[value.enum]
		if enumGroup == nil then
			return nil, ("unknown Enum type %q"):format(value.enum)
		end
		local enumValue = enumGroup[value.value]
		if enumValue == nil then
			return nil, ("unknown Enum value %q for Enum.%s"):format(value.value, value.enum)
		end
		return enumValue, nil
	elseif tag == "Ref" then
		local resolved = resolveByInstanceID(value.instanceId)
		if resolved ~= nil then
			return resolved, nil
		end
		if typeof(value.path) ~= "string" or value.path == "" then
			return nil, "Ref requires a non-empty 'path' field or valid 'instanceId'"
		end
		resolved = resolvePath(value.path)
		if resolved == nil then
			return nil, ("Ref path not found: %q"):format(value.path)
		end
		return resolved, nil
	elseif tag == "NumberRange" then
		return NumberRange.new(value.min or 0, value.max or 0), nil
	elseif tag == "NumberSequence" then
		local keypoints = value.keypoints
		if typeof(keypoints) ~= "table" or #keypoints == 0 then
			return nil, "NumberSequence requires keypoints array"
		end
		local out = {}
		for _, kp in ipairs(keypoints) do
			if typeof(kp) ~= "table" then
				return nil, "NumberSequence keypoint must be an object"
			end
			table.insert(out, NumberSequenceKeypoint.new(kp.time or 0, kp.value or 0, kp.envelope or 0))
		end
		return NumberSequence.new(out), nil
	elseif tag == "ColorSequence" then
		local keypoints = value.keypoints
		if typeof(keypoints) ~= "table" or #keypoints == 0 then
			return nil, "ColorSequence requires keypoints array"
		end
		local out = {}
		for _, kp in ipairs(keypoints) do
			if typeof(kp) ~= "table" then
				return nil, "ColorSequence keypoint must be an object"
			end
			local color = Color3.new(kp.r or 0, kp.g or 0, kp.b or 0)
			table.insert(out, ColorSequenceKeypoint.new(kp.time or 0, color))
		end
		return ColorSequence.new(out), nil
	elseif tag == "PhysicalProperties" then
		return PhysicalProperties.new(
			value.density or 0.7,
			value.friction or 0.3,
			value.elasticity or 0.5,
			value.frictionWeight or 1,
			value.elasticityWeight or 1
		), nil
	elseif tag == "Axes" then
		local axes = {}
		if value.x == true then
			table.insert(axes, Enum.Axis.X)
		end
		if value.y == true then
			table.insert(axes, Enum.Axis.Y)
		end
		if value.z == true then
			table.insert(axes, Enum.Axis.Z)
		end
		if #axes == 0 then
			return nil, "Axes requires at least one axis flag"
		end
		return Axes.new(table.unpack(axes)), nil
	elseif tag == "Faces" then
		local faces = {}
		if value.right == true then
			table.insert(faces, Enum.NormalId.Right)
		end
		if value.top == true then
			table.insert(faces, Enum.NormalId.Top)
		end
		if value.back == true then
			table.insert(faces, Enum.NormalId.Back)
		end
		if value.left == true then
			table.insert(faces, Enum.NormalId.Left)
		end
		if value.bottom == true then
			table.insert(faces, Enum.NormalId.Bottom)
		end
		if value.front == true then
			table.insert(faces, Enum.NormalId.Front)
		end
		if #faces == 0 then
			return nil, "Faces requires at least one normal flag"
		end
		return Faces.new(table.unpack(faces)), nil
	elseif tag == "Rect" then
		return Rect.new(value.x0 or 0, value.y0 or 0, value.x1 or 0, value.y1 or 0), nil
	end

	return nil, ("unknown _type %q"):format(tostring(tag))
end

function GoxBridge:_handleInstanceSetProperty(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local instancePath = payload.instancePath
	local propertyName, propertyNameErr = asString(payload.propertyName)
	if propertyName == nil then
		return Protocol.errorResponse(
			request,
			"VALIDATION_ERROR",
			"propertyName " .. (propertyNameErr :: string),
			false,
			nil
		)
	end
	local value = payload.value
	if value == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "value is required", false, nil)
	end

	local target, targetErr = resolveTarget(payload, "instancePath", "instanceId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			instancePath = instancePath,
			instanceId = payload.instanceId,
		})
	end

	local dryRun = payload.dryRun == true
	local resolvedPath = buildPath(target)
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("set %s.%s"):format(resolvedPath, propertyName),
		})
	end

	local coerced, coerceErr = coerceValue(value)
	if coerceErr then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "value coercion failed: " .. coerceErr, false, {
			propertyName = propertyName,
		})
	end

	local ok, setErr = pcall(function()
		(target :: any)[propertyName] = coerced
	end)
	if not ok then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "failed to set property", false, {
			propertyName = propertyName,
			error = tostring(setErr),
		})
	end

	return Protocol.okResponse(request, {
		updated = true,
		path = resolvedPath,
		instanceId = ensureInstanceId(target),
		propertyName = propertyName,
		diffSummary = ("set %s.%s"):format(resolvedPath, propertyName),
	})
end

function GoxBridge:_handleInstanceDelete(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local instancePath = payload.instancePath

	local target, targetErr = resolveTarget(payload, "instancePath", "instanceId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			instancePath = instancePath,
			instanceId = payload.instanceId,
		})
	end

	local dryRun = payload.dryRun == true
	local resolvedPath = buildPath(target)
	if dryRun then
		return Protocol.okResponse(request, {
			dryRun = true,
			diffSummary = ("delete %s"):format(resolvedPath),
		})
	end

	target:Destroy()
	return Protocol.okResponse(request, {
		deleted = true,
		path = resolvedPath,
		diffSummary = ("deleted %s"):format(resolvedPath),
	})
end

function GoxBridge:_handleInstanceGet(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local instancePath = payload.instancePath

	local target, targetErr = resolveTarget(payload, "instancePath", "instanceId")
	if target == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", targetErr :: string, false, {
			instancePath = instancePath,
			instanceId = payload.instanceId,
		})
	end

	return Protocol.okResponse(request, {
		instance = serializeInstance(target),
	})
end

function GoxBridge:_handleInstanceListChildren(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local parentPath = payload.parentPath

	local parent, parentErr = resolveTarget(payload, "parentPath", "parentId")
	if parent == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", parentErr :: string, false, {
			parentPath = parentPath,
			parentId = payload.parentId,
		})
	end

	local limit, limitErr = parseLimit(payload, 100)
	if limitErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", limitErr :: string, false, nil)
	end
	local offset, offsetErr = parseOffset(payload)
	if offsetErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", offsetErr :: string, false, nil)
	end

	local children = parent:GetChildren()
	local total = #children
	local startIndex = (offset :: number) + 1
	local endIndex = math.min(total, (offset :: number) + (limit :: number))
	local serializedChildren: { any } = {}
	for i = startIndex, endIndex do
		local child = children[i]
		if child == nil then
			break
		end
		table.insert(serializedChildren, serializeInstance(child))
	end
	local hasMore = endIndex < total
	local nextOffset = nil
	if hasMore then
		nextOffset = endIndex
	end

	return Protocol.okResponse(request, {
		parentPath = parentPath,
		offset = offset,
		limit = limit,
		total = total,
		count = #serializedChildren,
		hasMore = hasMore,
		nextOffset = nextOffset,
		children = serializedChildren,
	})
end

function GoxBridge:_handleInstanceFind(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local rootPath = payload.rootPath

	local nameContains, nameContainsErr = optionalNonEmptyString(payload, "nameContains")
	if nameContainsErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", nameContainsErr :: string, false, nil)
	end
	local className, classNameErr = optionalNonEmptyString(payload, "className")
	if classNameErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", classNameErr :: string, false, nil)
	end
	if nameContains == nil and className == nil then
		return Protocol.errorResponse(
			request,
			"VALIDATION_ERROR",
			"at least one filter is required: nameContains or className",
			false,
			nil
		)
	end

	local limit, limitErr = parseLimit(payload, 50)
	if limitErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", limitErr :: string, false, nil)
	end
	local offset, offsetErr = parseOffset(payload)
	if offsetErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", offsetErr :: string, false, nil)
	end

	local root, rootErr = resolveTarget(payload, "rootPath", "rootId")
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = rootPath,
			rootId = payload.rootId,
		})
	end

	local lowerNameContains = nil
	if nameContains ~= nil then
		lowerNameContains = string.lower(nameContains :: string)
	end

	local function matches(target: Instance): boolean
		if className ~= nil and target.ClassName ~= className then
			return false
		end
		if lowerNameContains ~= nil then
			local name = string.lower(target.Name)
			if string.find(name, lowerNameContains :: string, 1, true) == nil then
				return false
			end
		end
		return true
	end

	local matchesList: { any } = {}
	local hasMore = false
	local skippedMatches = 0
	local function pushIfMatch(target: Instance)
		if hasMore then
			return
		end
		if matches(target) then
			if skippedMatches < (offset :: number) then
				skippedMatches = skippedMatches + 1
				return
			end
			if #matchesList >= (limit :: number) then
				hasMore = true
				return
			end
			table.insert(matchesList, serializeInstance(target))
		end
	end

	pushIfMatch(root)
	if not hasMore then
		for _, descendant in ipairs(root:GetDescendants()) do
			pushIfMatch(descendant)
			if hasMore then
				break
			end
		end
	end
	local nextOffset = nil
	if hasMore then
		nextOffset = (offset :: number) + #matchesList
	end

	return Protocol.okResponse(request, {
		rootPath = rootPath,
		offset = offset,
		limit = limit,
		count = #matchesList,
		hasMore = hasMore,
		nextOffset = nextOffset,
		truncated = hasMore,
		matches = matchesList,
	})
end

return GoxBridge

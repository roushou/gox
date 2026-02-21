--!strict

local HttpService = game:GetService("HttpService")
local CollectionService = game:GetService("CollectionService")
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

local sceneIDAttribute = "gox_scene_id"
local sceneManagedAttribute = "gox_scene_managed"
local sceneManagedTag = "gox.scene.managed"

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

	if request.operation == "scene.snapshot" then
		return self:_handleSceneSnapshot(request)
	end

	if request.operation == "scene.plan" then
		return self:_handleScenePlan(request)
	end

	if request.operation == "scene.apply" then
		return self:_handleSceneApply(request)
	end

	if request.operation == "scene.validate" then
		return self:_handleSceneValidate(request)
	end

	if request.operation == "scene.capture" then
		return self:_handleSceneCapture(request)
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

local function asNonNegativeInteger(raw: any, defaultValue: number, maxValue: number): (number?, string?)
	if raw == nil then
		return defaultValue, nil
	end
	if typeof(raw) ~= "number" then
		return nil, "must be a number"
	end
	local n = math.floor(raw :: number)
	if n < 0 then
		return nil, "must be >= 0"
	end
	if n > maxValue then
		n = maxValue
	end
	return n, nil
end

local function asPositiveInteger(raw: any, defaultValue: number, maxValue: number): (number?, string?)
	if raw == nil then
		return defaultValue, nil
	end
	if typeof(raw) ~= "number" then
		return nil, "must be a number"
	end
	local n = math.floor(raw :: number)
	if n < 1 then
		return nil, "must be >= 1"
	end
	if n > maxValue then
		n = maxValue
	end
	return n, nil
end

local function toColorRGB(color: Color3): { [string]: any }
	return {
		r = math.floor(color.R * 255 + 0.5),
		g = math.floor(color.G * 255 + 0.5),
		b = math.floor(color.B * 255 + 0.5),
	}
end

local function toVector3(v: Vector3): { [string]: any }
	return {
		x = v.X,
		y = v.Y,
		z = v.Z,
	}
end

local function toCFrame(cf: CFrame): { [string]: any }
	local components = { cf:GetComponents() }
	return {
		x = cf.X,
		y = cf.Y,
		z = cf.Z,
		components = components,
	}
end

local function readAttributes(instance: Instance): { [string]: any }
	local ok, attrsOrErr = pcall(function()
		return instance:GetAttributes()
	end)
	if not ok or typeof(attrsOrErr) ~= "table" then
		return {}
	end
	local attrs = attrsOrErr :: { [string]: any }
	local out: { [string]: any } = {}
	for key, value in pairs(attrs) do
		out[tostring(key)] = normalizeReturnValue(value, 0)
	end
	return out
end

local function readTags(instance: Instance): { string }
	local ok, tagsOrErr = pcall(function()
		return CollectionService:GetTags(instance)
	end)
	if not ok or typeof(tagsOrErr) ~= "table" then
		return {}
	end
	local tags = tagsOrErr :: { string }
	table.sort(tags)
	return tags
end

local function keySetFromList(list: { string }): { [string]: boolean }
	local out: { [string]: boolean } = {}
	for _, value in ipairs(list) do
		out[value] = true
	end
	return out
end

local function listsEqual(a: { string }, b: { string }): boolean
	if #a ~= #b then
		return false
	end
	for i, value in ipairs(a) do
		if b[i] ~= value then
			return false
		end
	end
	return true
end

local function mapsEqual(a: { [string]: any }, b: { [string]: any }): boolean
	for key, value in pairs(a) do
		if b[key] ~= value then
			return false
		end
	end
	for key, value in pairs(b) do
		if a[key] ~= value then
			return false
		end
	end
	return true
end

local function sceneProperties(instance: Instance): { [string]: any }
	local out: { [string]: any } = {}
	if instance:IsA("BasePart") then
		local part = instance :: BasePart
		out.Anchored = part.Anchored
		out.CanCollide = part.CanCollide
		out.Material = part.Material.Name
		out.Color = toColorRGB(part.Color)
		out.Transparency = part.Transparency
		out.Size = toVector3(part.Size)
		out.CFrame = toCFrame(part.CFrame)
	elseif instance:IsA("Model") then
		local model = instance :: Model
		if model.PrimaryPart ~= nil then
			out.PrimaryPartId = ensureInstanceId(model.PrimaryPart :: Instance)
		end
	end
	return out
end

local function defaultSceneSnapshotOptions(payload: { [string]: any }): ({ [string]: any }?, string?)
	local maxDepth, maxDepthErr = asNonNegativeInteger(payload.maxDepth, 6, 32)
	if maxDepthErr ~= nil then
		return nil, "maxDepth " .. (maxDepthErr :: string)
	end
	local maxChildren, maxChildrenErr = asPositiveInteger(payload.maxChildrenPerNode, 200, 500)
	if maxChildrenErr ~= nil then
		return nil, "maxChildrenPerNode " .. (maxChildrenErr :: string)
	end
	return {
		maxDepth = maxDepth,
		maxChildrenPerNode = maxChildren,
		includeProperties = payload.includeProperties ~= false,
		includeAttributes = payload.includeAttributes ~= false,
		includeTags = payload.includeTags ~= false,
	}, nil
end

local function updateBounds(
	metrics: { [string]: any },
	center: Vector3,
	size: Vector3
)
	local half = size / 2
	local minV = center - half
	local maxV = center + half
	if metrics.boundsMin == nil then
		metrics.boundsMin = minV
		metrics.boundsMax = maxV
		return
	end
	local curMin = metrics.boundsMin :: Vector3
	local curMax = metrics.boundsMax :: Vector3
	metrics.boundsMin = Vector3.new(
		math.min(curMin.X, minV.X),
		math.min(curMin.Y, minV.Y),
		math.min(curMin.Z, minV.Z)
	)
	metrics.boundsMax = Vector3.new(
		math.max(curMax.X, maxV.X),
		math.max(curMax.Y, maxV.Y),
		math.max(curMax.Z, maxV.Z)
	)
end

local function serializeSceneNode(instance: Instance, options: { [string]: any }, depth: number, metrics: { [string]: any }): { [string]: any }
	metrics.nodeCount = (metrics.nodeCount :: number) + 1
	if instance:IsA("BasePart") then
		metrics.partCount = (metrics.partCount :: number) + 1
		local part = instance :: BasePart
		updateBounds(metrics, part.Position, part.Size)
		local color = toColorRGB(part.Color)
		local colorKey = tostring(color.r) .. ":" .. tostring(color.g) .. ":" .. tostring(color.b)
		local uniqueColors = metrics.uniqueColors :: { [string]: boolean }
		uniqueColors[colorKey] = true
	end
	if instance:IsA("Model") then
		metrics.modelCount = (metrics.modelCount :: number) + 1
	end

	local out = serializeInstance(instance)
	out.sceneId = instance:GetAttribute(sceneIDAttribute)
	out.managed = instance:GetAttribute(sceneManagedAttribute) == true
	if options.includeProperties == true then
		out.properties = sceneProperties(instance)
	end
	if options.includeAttributes == true then
		out.attributes = readAttributes(instance)
	end
	if options.includeTags == true then
		out.tags = readTags(instance)
	end

	local maxDepth = options.maxDepth :: number
	if depth >= maxDepth then
		out.truncated = #instance:GetChildren() > 0
		return out
	end

	local children = instance:GetChildren()
	local maxChildren = options.maxChildrenPerNode :: number
	local serializedChildren: { any } = {}
	local limit = math.min(#children, maxChildren)
	for i = 1, limit do
		table.insert(serializedChildren, serializeSceneNode(children[i], options, depth + 1, metrics))
	end
	out.children = serializedChildren
	if #children > maxChildren then
		out.truncatedChildren = #children - maxChildren
	end
	return out
end

local function sceneRootFromPayload(payload: { [string]: any }): (Instance?, string?)
	return resolveTarget(payload, "rootPath", "rootId")
end

local function indexSceneByID(root: Instance): { [string]: Instance }
	local out: { [string]: Instance } = {}
	local function pushInstance(instance: Instance)
		local rawID = instance:GetAttribute(sceneIDAttribute)
		if typeof(rawID) == "string" and rawID ~= "" then
			out[rawID] = instance
		end
	end
	pushInstance(root)
	for _, descendant in ipairs(root:GetDescendants()) do
		pushInstance(descendant)
	end
	return out
end

local function parseSceneDefinition(definition: any): ({ any }?, string?)
	if typeof(definition) ~= "table" then
		return nil, "definition must be an object"
	end
	local nodes = (definition :: any).nodes
	if typeof(nodes) ~= "table" then
		return nil, "definition.nodes must be an array"
	end
	local out: { any } = {}
	local seen: { [string]: boolean } = {}
	for _, rawNode in ipairs(nodes :: { any }) do
		if typeof(rawNode) ~= "table" then
			return nil, "definition.nodes entries must be objects"
		end
		local node = rawNode :: { [string]: any }
		local id = node.id
		local className = node.className
		if typeof(id) ~= "string" or id == "" then
			return nil, "each node.id must be a non-empty string"
		end
		if typeof(className) ~= "string" or className == "" then
			return nil, ("node %q must include className"):format(id)
		end
		if seen[id] then
			return nil, ("duplicate node id %q"):format(id)
		end
		seen[id] = true

		local name = node.name
		if typeof(name) ~= "string" or name == "" then
			name = id
		end
		local parentId = node.parentId
		if typeof(parentId) ~= "string" or parentId == "" then
			parentId = "root"
		end

		local properties = {}
		if typeof(node.properties) == "table" then
			properties = node.properties
		end

		local attributes = {}
		if typeof(node.attributes) == "table" then
			attributes = node.attributes
		end

		local tags: { string } = {}
		if typeof(node.tags) == "table" then
			for _, rawTag in ipairs(node.tags :: { any }) do
				if typeof(rawTag) == "string" and rawTag ~= "" then
					table.insert(tags, rawTag)
				end
			end
			table.sort(tags)
		end

		table.insert(out, {
			id = id,
			className = className,
			name = name,
			parentId = parentId,
			properties = properties,
			attributes = attributes,
			tags = tags,
		})
	end

	table.sort(out, function(a, b)
		return (a.id :: string) < (b.id :: string)
	end)
	return out, nil
end

local function buildScenePlan(root: Instance, payload: { [string]: any }): ({ [string]: any }?, string?)
	local desiredNodes, parseErr = parseSceneDefinition(payload.definition)
	if desiredNodes == nil then
		return nil, parseErr
	end
	local allowDelete = payload.allowDelete == true
	local maxOps, maxOpsErr = asPositiveInteger(payload.maxOps, 2000, 5000)
	if maxOpsErr ~= nil then
		return nil, "maxOps " .. (maxOpsErr :: string)
	end
	local seed = payload.seed
	if seed == nil then
		seed = ""
	end

	local byID = indexSceneByID(root)
	local desiredByID: { [string]: boolean } = {}
	local steps: { any } = {}
	local summary = {
		create = 0,
		update = 0,
		replace = 0,
		delete = 0,
		noop = 0,
		error = 0,
	}

	for _, node in ipairs(desiredNodes :: { any }) do
		local nodeID = node.id :: string
		desiredByID[nodeID] = true
		local current = byID[nodeID]
		local parentId = node.parentId :: string
		local parentInstance = root
		if parentId ~= "root" then
			parentInstance = byID[parentId]
			if parentInstance == nil then
				summary.error += 1
				table.insert(steps, {
					op = "error",
					id = nodeID,
					reason = "parent_missing",
					message = ("node %q references missing parentId %q"):format(nodeID, parentId),
				})
				continue
			end
		end

		if current == nil then
			summary.create += 1
			table.insert(steps, {
				op = "create",
				id = nodeID,
				risk = "medium",
				node = node,
			})
		elseif current.ClassName ~= (node.className :: string) then
			summary.replace += 1
			table.insert(steps, {
				op = "replace",
				id = nodeID,
				risk = "high",
				node = node,
			})
		else
			local hasUpdate = false
			if current.Name ~= (node.name :: string) then
				hasUpdate = true
			end
			if current.Parent ~= parentInstance then
				hasUpdate = true
			end
			local properties = node.properties :: { [string]: any }
			if next(properties) ~= nil then
				hasUpdate = true
			end
			local attrs = node.attributes :: { [string]: any }
			local existingAttrs = readAttributes(current)
			if not mapsEqual(existingAttrs, attrs) then
				hasUpdate = true
			end
			local tags = node.tags :: { string }
			if not listsEqual(readTags(current), tags) then
				hasUpdate = true
			end

			if hasUpdate then
				summary.update += 1
				table.insert(steps, {
					op = "update",
					id = nodeID,
					risk = "low",
					node = node,
				})
			else
				summary.noop += 1
				table.insert(steps, {
					op = "noop",
					id = nodeID,
					risk = "none",
				})
			end
		end
	end

	if allowDelete then
		local staleIDs: { string } = {}
		for existingID, _ in pairs(byID) do
			if existingID ~= "root" and not desiredByID[existingID] then
				table.insert(staleIDs, existingID)
			end
		end
		table.sort(staleIDs)
		for _, staleID in ipairs(staleIDs) do
			summary.delete += 1
			table.insert(steps, {
				op = "delete",
				id = staleID,
				risk = "high",
			})
		end
	end

	local maxOpsLimit = maxOps :: number
	local truncated = false
	if #steps > maxOpsLimit then
		truncated = true
		while #steps > maxOpsLimit do
			table.remove(steps)
		end
	end

	local ids: { string } = {}
	for _, step in ipairs(steps) do
		if typeof(step.id) == "string" then
			table.insert(ids, step.id)
		end
	end
	local planHash = table.concat(ids, "|") .. "|" .. tostring(seed)

	return {
		version = "scene-plan.v1",
		rootPath = buildPath(root),
		rootInstanceId = ensureInstanceId(root),
		seed = seed,
		allowDelete = allowDelete,
		generatedAt = os.date("!%Y-%m-%dT%H:%M:%SZ"),
		planHash = planHash,
		truncated = truncated,
		steps = steps,
		summary = summary,
	}, nil
end

local function ensureManagedInstance(instance: Instance, sceneID: string)
	instance:SetAttribute(sceneIDAttribute, sceneID)
	instance:SetAttribute(sceneManagedAttribute, true)
	local hasTag = CollectionService:HasTag(instance, sceneManagedTag)
	if not hasTag then
		CollectionService:AddTag(instance, sceneManagedTag)
	end
end

local function setInstanceAttributes(instance: Instance, attrs: { [string]: any })
	local existing = readAttributes(instance)
	for key, _ in pairs(existing) do
		if attrs[key] == nil then
			instance:SetAttribute(key, nil)
		end
	end
	for key, value in pairs(attrs) do
		instance:SetAttribute(key, value)
	end
end

local function setInstanceTags(instance: Instance, tags: { string })
	local desired = keySetFromList(tags)
	for _, currentTag in ipairs(readTags(instance)) do
		if not desired[currentTag] then
			CollectionService:RemoveTag(instance, currentTag)
		end
	end
	for _, nextTag in ipairs(tags) do
		if not CollectionService:HasTag(instance, nextTag) then
			CollectionService:AddTag(instance, nextTag)
		end
	end
end

local function ensureSceneTrashFolder(root: Instance): Folder
	local existing = root:FindFirstChild("_GoxTrash")
	if existing ~= nil and existing:IsA("Folder") then
		return existing :: Folder
	end
	local folder = Instance.new("Folder")
	folder.Name = "_GoxTrash"
	folder.Parent = root
	return folder
end

local function rollbackSceneActions(actions: { any })
	for i = #actions, 1, -1 do
		local item = actions[i]
		local kind = item.kind
		if kind == "destroy" then
			local instance = item.instance
			if typeof(instance) == "Instance" and (instance :: Instance).Parent ~= nil then
				pcall(function()
					(instance :: Instance):Destroy()
				end)
			end
		elseif kind == "reparent" then
			if typeof(item.instance) == "Instance" then
				pcall(function()
					(item.instance :: Instance).Parent = item.parent
				end)
			end
		elseif kind == "set_property" then
			if typeof(item.instance) == "Instance" and typeof(item.propertyName) == "string" then
				pcall(function()
					(item.instance :: any)[item.propertyName] = item.oldValue
				end)
			end
		elseif kind == "restore_attributes" then
			if typeof(item.instance) == "Instance" and typeof(item.oldAttributes) == "table" then
				pcall(function()
					setInstanceAttributes(item.instance :: Instance, item.oldAttributes)
				end)
			end
		elseif kind == "restore_tags" then
			if typeof(item.instance) == "Instance" and typeof(item.oldTags) == "table" then
				pcall(function()
					setInstanceTags(item.instance :: Instance, item.oldTags)
				end)
			end
		end
	end
end

local function resolveParentForNode(root: Instance, byID: { [string]: Instance }, node: { [string]: any }): (Instance?, string?)
	local parentID = node.parentId
	if typeof(parentID) ~= "string" or parentID == "" or parentID == "root" then
		return root, nil
	end
	local parent = byID[parentID]
	if parent == nil then
		return nil, ("parentId %q not found"):format(parentID)
	end
	return parent, nil
end

local function applyNodeProperties(instance: Instance, properties: { [string]: any }, rollbackActions: { any }): string?
	for propertyName, rawValue in pairs(properties) do
		local okRead, oldValue = pcall(function()
			return (instance :: any)[propertyName]
		end)
		if okRead then
			table.insert(rollbackActions, {
				kind = "set_property",
				instance = instance,
				propertyName = propertyName,
				oldValue = oldValue,
			})
		end

		local coerced, coerceErr = coerceValue(rawValue)
		if coerceErr ~= nil then
			return ("value coercion failed for %s: %s"):format(propertyName, coerceErr)
		end
		local okSet, setErr = pcall(function()
			(instance :: any)[propertyName] = coerced
		end)
		if not okSet then
			return ("failed to set %s: %s"):format(propertyName, tostring(setErr))
		end
	end
	return nil
end

local function executeScenePlan(root: Instance, plan: { [string]: any }, dryRun: boolean): ({ [string]: any }?, string?)
	local steps = plan.steps
	if typeof(steps) ~= "table" then
		return nil, "plan.steps must be an array"
	end

	if dryRun then
		return {
			applied = false,
			dryRun = true,
			rolledBack = false,
			executedSteps = 0,
			totalSteps = #steps,
			diffSummary = ("would apply %d scene steps under %s"):format(#steps, buildPath(root)),
		}, nil
	end

	local byID = indexSceneByID(root)
	local trashFolder = ensureSceneTrashFolder(root)
	local rollbackActions: { any } = {}
	local executedSteps: { any } = {}
	local rollbackOnError = plan.rollbackOnError ~= false

	for _, step in ipairs(steps :: { any }) do
		local op = step.op
		if op == "noop" then
			table.insert(executedSteps, { id = step.id, op = op, status = "noop" })
		elseif op == "error" then
			if rollbackOnError then
				rollbackSceneActions(rollbackActions)
			end
			return nil, step.message or "plan contains error step"
		elseif op == "delete" then
			local id = step.id
			local target = byID[id]
			if target ~= nil and target ~= root then
				table.insert(rollbackActions, {
					kind = "reparent",
					instance = target,
					parent = target.Parent,
				})
				target.Parent = trashFolder
				byID[id] = nil
			end
			table.insert(executedSteps, { id = id, op = op, status = "ok" })
		else
			local node = step.node
			if typeof(node) ~= "table" then
				if rollbackOnError then
					rollbackSceneActions(rollbackActions)
				end
				return nil, ("step %q missing node payload"):format(tostring(step.id))
			end

			local id = node.id :: string
			local parent, parentErr = resolveParentForNode(root, byID, node)
			if parent == nil then
				if rollbackOnError then
					rollbackSceneActions(rollbackActions)
				end
				return nil, parentErr or "parent resolution failed"
			end

			local target = byID[id]
			if op == "create" or op == "replace" then
				if op == "replace" and target ~= nil and target ~= root then
					table.insert(rollbackActions, {
						kind = "reparent",
						instance = target,
						parent = target.Parent,
					})
					target.Parent = trashFolder
				end
				local okCreate, createdOrErr = pcall(function()
					return Instance.new(node.className)
				end)
				if not okCreate then
					if rollbackOnError then
						rollbackSceneActions(rollbackActions)
					end
					return nil, ("invalid className %q"):format(tostring(node.className))
				end
				target = createdOrErr :: Instance
				table.insert(rollbackActions, { kind = "destroy", instance = target })
				target.Name = node.name
				target.Parent = parent
				ensureManagedInstance(target, id)
				byID[id] = target
			elseif target == nil then
				if rollbackOnError then
					rollbackSceneActions(rollbackActions)
				end
				return nil, ("node %q is missing for update"):format(id)
			end

			if target.Parent ~= parent then
				table.insert(rollbackActions, {
					kind = "reparent",
					instance = target,
					parent = target.Parent,
				})
				target.Parent = parent
			end
			if target.Name ~= (node.name :: string) then
				table.insert(rollbackActions, {
					kind = "set_property",
					instance = target,
					propertyName = "Name",
					oldValue = target.Name,
				})
				target.Name = node.name
			end

			table.insert(rollbackActions, {
				kind = "restore_attributes",
				instance = target,
				oldAttributes = readAttributes(target),
			})
			setInstanceAttributes(target, node.attributes or {})

			table.insert(rollbackActions, {
				kind = "restore_tags",
				instance = target,
				oldTags = readTags(target),
			})
			local desiredTags = node.tags
			if typeof(desiredTags) ~= "table" then
				desiredTags = {}
			end
			setInstanceTags(target, desiredTags)

			local propertyErr = applyNodeProperties(target, node.properties or {}, rollbackActions)
			if propertyErr ~= nil then
				if rollbackOnError then
					rollbackSceneActions(rollbackActions)
				end
				return nil, propertyErr
			end

			table.insert(executedSteps, { id = id, op = op, status = "ok" })
		end
	end

	return {
		applied = true,
		rolledBack = false,
		executedSteps = #executedSteps,
		totalSteps = #steps,
		steps = executedSteps,
		diffSummary = ("applied %d scene steps under %s"):format(#executedSteps, buildPath(root)),
	}, nil
end

function GoxBridge:_handleSceneSnapshot(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local root, rootErr = sceneRootFromPayload(payload)
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = payload.rootPath,
			rootId = payload.rootId,
		})
	end

	local options, optionErr = defaultSceneSnapshotOptions(payload)
	if options == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", optionErr :: string, false, nil)
	end

	local metrics = {
		nodeCount = 0,
		partCount = 0,
		modelCount = 0,
		boundsMin = nil,
		boundsMax = nil,
		uniqueColors = {},
	}
	local snapshot = serializeSceneNode(root, options, 0, metrics)
	local bounds = nil
	if metrics.boundsMin ~= nil and metrics.boundsMax ~= nil then
		local minV = metrics.boundsMin :: Vector3
		local maxV = metrics.boundsMax :: Vector3
		bounds = {
			min = toVector3(minV),
			max = toVector3(maxV),
			size = toVector3(maxV - minV),
			center = toVector3((maxV + minV) / 2),
		}
	end

	local colorCount = 0
	for _, _ in pairs(metrics.uniqueColors :: { [string]: boolean }) do
		colorCount += 1
	end

	return Protocol.okResponse(request, {
		rootPath = buildPath(root),
		rootInstanceId = ensureInstanceId(root),
		nodeCount = metrics.nodeCount,
		partCount = metrics.partCount,
		modelCount = metrics.modelCount,
		uniqueColorCount = colorCount,
		bounds = bounds,
		snapshot = snapshot,
	})
end

function GoxBridge:_handleScenePlan(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local root, rootErr = sceneRootFromPayload(payload)
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = payload.rootPath,
			rootId = payload.rootId,
		})
	end
	local plan, planErr = buildScenePlan(root, payload)
	if plan == nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", planErr :: string, false, nil)
	end
	return Protocol.okResponse(request, plan)
end

function GoxBridge:_handleSceneApply(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local root, rootErr = sceneRootFromPayload(payload)
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = payload.rootPath,
			rootId = payload.rootId,
		})
	end

	local plan = payload.plan
	if typeof(plan) ~= "table" then
		local autoPlan, planErr = buildScenePlan(root, payload)
		if autoPlan == nil then
			return Protocol.errorResponse(request, "VALIDATION_ERROR", planErr :: string, false, nil)
		end
		plan = autoPlan
	end
	local mutablePlan = plan :: any
	mutablePlan.rollbackOnError = payload.rollbackOnError ~= false

	local out, applyErr = executeScenePlan(root, plan, payload.dryRun == true)
	if out == nil then
		return Protocol.errorResponse(request, "INTERNAL_ERROR", applyErr or "scene apply failed", true, {
			rootPath = buildPath(root),
		})
	end
	out.plan = plan
	return Protocol.okResponse(request, out)
end

local function intersectsAABB(aMin: Vector3, aMax: Vector3, bMin: Vector3, bMax: Vector3): boolean
	return aMin.X <= bMax.X
		and aMax.X >= bMin.X
		and aMin.Y <= bMax.Y
		and aMax.Y >= bMin.Y
		and aMin.Z <= bMax.Z
		and aMax.Z >= bMin.Z
end

function GoxBridge:_handleSceneValidate(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local root, rootErr = sceneRootFromPayload(payload)
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = payload.rootPath,
			rootId = payload.rootId,
		})
	end

	local rules = payload.rules
	if typeof(rules) ~= "table" then
		rules = {}
	end
	local maxPartChecks, maxPartChecksErr = asPositiveInteger(payload.maxPartChecks, 250, 600)
	if maxPartChecksErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "maxPartChecks " .. (maxPartChecksErr :: string), false, nil)
	end
	local maxUniqueColors = rules.maxUniqueColors
	if typeof(maxUniqueColors) ~= "number" then
		maxUniqueColors = 12
	end
	local maxUnanchoredParts = rules.maxUnanchoredParts
	if typeof(maxUnanchoredParts) ~= "number" then
		maxUnanchoredParts = 25
	end
	local maxPartSize = rules.maxPartSize
	if typeof(maxPartSize) ~= "number" then
		maxPartSize = 2048
	end
	local minPartSize = rules.minPartSize
	if typeof(minPartSize) ~= "number" then
		minPartSize = 0.05
	end

	local issues: { any } = {}
	local function pushIssue(severity: string, code: string, message: string, path: string?, details: { [string]: any }?)
		table.insert(issues, {
			severity = severity,
			code = code,
			message = message,
			path = path or "",
			details = details or {},
		})
	end

	local parts: { BasePart } = {}
	if root:IsA("BasePart") then
		table.insert(parts, root :: BasePart)
	end
	for _, desc in ipairs(root:GetDescendants()) do
		if desc:IsA("BasePart") then
			if #parts >= (maxPartChecks :: number) then
				break
			end
			table.insert(parts, desc :: BasePart)
		end
	end

	local uniqueColors: { [string]: boolean } = {}
	local unanchoredCount = 0
	for _, part in ipairs(parts) do
		local color = toColorRGB(part.Color)
		local colorKey = tostring(color.r) .. ":" .. tostring(color.g) .. ":" .. tostring(color.b)
		uniqueColors[colorKey] = true
		if part.Anchored ~= true then
			unanchoredCount += 1
		end
		if part.Size.X > (maxPartSize :: number) or part.Size.Y > (maxPartSize :: number) or part.Size.Z > (maxPartSize :: number) then
			pushIssue("error", "oversized_part", "part exceeds maxPartSize", buildPath(part), {
				size = toVector3(part.Size),
				maxPartSize = maxPartSize,
			})
		end
		if part.Size.X < (minPartSize :: number) or part.Size.Y < (minPartSize :: number) or part.Size.Z < (minPartSize :: number) then
			pushIssue("warning", "tiny_part", "part is below minPartSize", buildPath(part), {
				size = toVector3(part.Size),
				minPartSize = minPartSize,
			})
		end
	end

	local uniqueColorCount = 0
	for _, _ in pairs(uniqueColors) do
		uniqueColorCount += 1
	end
	if uniqueColorCount > (maxUniqueColors :: number) then
		pushIssue("warning", "palette_diversity", "scene exceeds unique color budget", buildPath(root), {
			uniqueColorCount = uniqueColorCount,
			maxUniqueColors = maxUniqueColors,
		})
	end
	if unanchoredCount > (maxUnanchoredParts :: number) then
		pushIssue("warning", "unanchored_density", "too many unanchored parts", buildPath(root), {
			unanchoredCount = unanchoredCount,
			maxUnanchoredParts = maxUnanchoredParts,
		})
	end

	local overlapChecks = math.min(#parts, 120)
	for i = 1, overlapChecks do
		local a = parts[i]
		local aHalf = a.Size / 2
		local aMin = a.Position - aHalf
		local aMax = a.Position + aHalf
		for j = i + 1, overlapChecks do
			local b = parts[j]
			local bHalf = b.Size / 2
			local bMin = b.Position - bHalf
			local bMax = b.Position + bHalf
			if intersectsAABB(aMin, aMax, bMin, bMax) then
				pushIssue("warning", "overlap", "part bounds overlap", buildPath(a), {
					other = buildPath(b),
				})
				if #issues >= 200 then
					break
				end
			end
		end
		if #issues >= 200 then
			break
		end
	end

	local modelsChecked = 0
	for _, instance in ipairs(root:GetDescendants()) do
		if instance:IsA("Model") then
			modelsChecked += 1
			local model = instance :: Model
			local partCount = 0
			for _, d in ipairs(model:GetDescendants()) do
				if d:IsA("BasePart") then
					partCount += 1
					if partCount > 1 then
						break
					end
				end
			end
			if partCount > 1 and model.PrimaryPart == nil then
				pushIssue("warning", "missing_primary_part", "model has multiple parts but no PrimaryPart", buildPath(model), nil)
			end
		end
	end

	local hasErrors = false
	for _, issue in ipairs(issues) do
		if issue.severity == "error" then
			hasErrors = true
			break
		end
	end

	return Protocol.okResponse(request, {
		rootPath = buildPath(root),
		valid = not hasErrors,
		issueCount = #issues,
		issues = issues,
		metrics = {
			partCount = #parts,
			uniqueColorCount = uniqueColorCount,
			unanchoredCount = unanchoredCount,
			modelsChecked = modelsChecked,
			overlapChecks = overlapChecks,
		},
	})
end

local function suggestCaptureCamera(root: Instance): { [string]: any }
	local metrics = {
		boundsMin = nil,
		boundsMax = nil,
	}
	local function pushPart(part: BasePart)
		updateBounds(metrics, part.Position, part.Size)
	end
	if root:IsA("BasePart") then
		pushPart(root :: BasePart)
	end
	for _, desc in ipairs(root:GetDescendants()) do
		if desc:IsA("BasePart") then
			pushPart(desc :: BasePart)
		end
	end

	if metrics.boundsMin == nil or metrics.boundsMax == nil then
		return {
			position = { x = 0, y = 20, z = 20 },
			lookAt = { x = 0, y = 0, z = 0 },
		}
	end
	local minV = metrics.boundsMin :: Vector3
	local maxV = metrics.boundsMax :: Vector3
	local center = (minV + maxV) / 2
	local size = maxV - minV
	local radius = math.max(size.X, size.Y, size.Z)
	if radius < 8 then
		radius = 8
	end
	local position = center + Vector3.new(radius * 1.4, radius, radius * 1.4)
	return {
		position = toVector3(position),
		lookAt = toVector3(center),
	}
end

function GoxBridge:_handleSceneCapture(request: BridgeRequest): BridgeResponse
	local payload = request.payload or {}
	local root, rootErr = sceneRootFromPayload(payload)
	if root == nil then
		return Protocol.errorResponse(request, "NOT_FOUND", rootErr :: string, false, {
			rootPath = payload.rootPath,
			rootId = payload.rootId,
		})
	end

	local width, widthErr = asPositiveInteger(payload.width, 1024, 4096)
	if widthErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "width " .. (widthErr :: string), false, nil)
	end
	local height, heightErr = asPositiveInteger(payload.height, 768, 4096)
	if heightErr ~= nil then
		return Protocol.errorResponse(request, "VALIDATION_ERROR", "height " .. (heightErr :: string), false, nil)
	end
	local format = "png"
	if typeof(payload.format) == "string" and payload.format ~= "" then
		format = string.lower(payload.format)
	end
	local suggestedCamera = suggestCaptureCamera(root)

	local thumbServiceOk, thumbServiceOrErr = pcall(function()
		return game:GetService("ThumbnailGenerator")
	end)
	if thumbServiceOk and thumbServiceOrErr ~= nil then
		local thumbService = thumbServiceOrErr
		local renderOk, rendered = pcall(function()
			return (thumbService :: any):Click(string.upper(format), width, height, true)
		end)
		if renderOk and typeof(rendered) == "string" and rendered ~= "" then
			return Protocol.okResponse(request, {
				captured = true,
				format = format,
				width = width,
				height = height,
				encoding = "base64",
				image = rendered,
				suggestedCamera = suggestedCamera,
				rootPath = buildPath(root),
			})
		end
	end

	return Protocol.okResponse(request, {
		captured = false,
		reason = "ThumbnailGenerator capture is unavailable in this Studio context",
		format = format,
		width = width,
		height = height,
		suggestedCamera = suggestedCamera,
		rootPath = buildPath(root),
	})
end

return GoxBridge

--!strict

export type BridgeError = {
	code: string,
	message: string,
	retryable: boolean?,
	details: { [string]: any }?,
}

export type BridgeRequest = {
	requestId: string,
	correlationId: string,
	operation: string,
	idempotencyKey: string?,
	payload: { [string]: any }?,
	timestamp: string,
}

export type BridgeResponse = {
	requestId: string,
	correlationId: string,
	success: boolean,
	payload: { [string]: any }?,
	error: BridgeError?,
	timestamp: string,
}

local Protocol = {}

local function nowIso(): string
	return os.date("!%Y-%m-%dT%H:%M:%SZ")
end

function Protocol.okResponse(request: BridgeRequest, payload: { [string]: any }?): BridgeResponse
	return {
		requestId = request.requestId,
		correlationId = request.correlationId,
		success = true,
		payload = payload,
		error = nil,
		timestamp = nowIso(),
	}
end

function Protocol.errorResponse(
	request: BridgeRequest,
	code: string,
	message: string,
	retryable: boolean?,
	details: { [string]: any }?
): BridgeResponse
	return {
		requestId = request.requestId,
		correlationId = request.correlationId,
		success = false,
		payload = nil,
		error = {
			code = code,
			message = message,
			retryable = retryable,
			details = details,
		},
		timestamp = nowIso(),
	}
end

return Protocol

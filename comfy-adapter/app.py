from __future__ import annotations

import base64
import asyncio
import ipaddress
import json
import mimetypes
import os
import secrets
import socket
from dataclasses import dataclass
from typing import Any
from urllib.parse import quote, urlencode, urlsplit

import httpx
from fastapi import Depends, FastAPI, Header, HTTPException, Response
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from workflow import compile_workflow, load_registry, public_model, public_workflow


@dataclass(frozen=True)
class Provider:
    id: str
    name: str
    base_url: str
    api_key: str = ""


class JobRequest(BaseModel):
    provider_id: str = "default"
    workflow_key: str
    prompt: str
    negative_prompt: str = ""
    input_images: list[str] = Field(default_factory=list, max_length=9)
    width: int | None = Field(default=None, ge=64, le=8192)
    height: int | None = Field(default=None, ge=64, le=8192)
    duration: float | None = Field(default=None, gt=0, le=60)
    generate_audio: bool | None = None
    seed: int | None = Field(default=None, ge=0)
    batch_size: int = Field(default=1, ge=1, le=4)
    denoise: float | None = Field(default=None, ge=0, le=1)
    metadata: dict[str, Any] = Field(default_factory=dict)


app = FastAPI(title="Canvas ComfyUI Workflow Adapter", version="1.0.0")


def providers() -> dict[str, Provider]:
    raw = os.getenv("COMFY_PROVIDERS_JSON", "").strip()
    rows: list[dict[str, Any]] = []
    if raw:
        parsed = json.loads(raw)
        rows = parsed if isinstance(parsed, list) else []
    elif os.getenv("COMFY_URL", "").strip():
        rows = [{"id": "default", "name": "ComfyUI", "base_url": os.getenv("COMFY_URL"), "api_key": os.getenv("COMFY_API_KEY", "")}]
    result: dict[str, Provider] = {}
    for row in rows:
        provider = Provider(id=str(row.get("id") or "").strip(), name=str(row.get("name") or row.get("id") or "ComfyUI"), base_url=str(row.get("base_url") or "").strip().rstrip("/"), api_key=str(row.get("api_key") or ""))
        if provider.id and provider.base_url.startswith(("http://", "https://")):
            result[provider.id] = provider
    return result


def require_token(authorization: str | None = Header(default=None)) -> None:
    expected = os.getenv("COMFY_ADAPTER_TOKEN", "").strip()
    if expected and not secrets.compare_digest(authorization or "", f"Bearer {expected}"):
        raise HTTPException(status_code=401, detail="invalid adapter token")


def provider_headers(provider: Provider) -> dict[str, str]:
    return {"Authorization": f"Bearer {provider.api_key}"} if provider.api_key else {}


def input_host_allowlist() -> set[str]:
    return {value.strip().lower().rstrip(".") for value in os.getenv("COMFY_INPUT_HOST_ALLOWLIST", "").split(",") if value.strip()}


async def validate_input_url(value: str) -> None:
    parsed = urlsplit(value)
    host = (parsed.hostname or "").lower().rstrip(".")
    if parsed.scheme not in {"http", "https"} or not host or parsed.username or parsed.password:
        raise HTTPException(status_code=400, detail="reference image URL is invalid")
    if host in input_host_allowlist():
        return
    try:
        infos = await asyncio.to_thread(socket.getaddrinfo, host, parsed.port or (443 if parsed.scheme == "https" else 80), type=socket.SOCK_STREAM)
    except OSError as exc:
        raise HTTPException(status_code=400, detail="reference image host cannot be resolved") from exc
    if not infos:
        raise HTTPException(status_code=400, detail="reference image host cannot be resolved")
    for info in infos:
        address = ipaddress.ip_address(info[4][0])
        if not address.is_global:
            raise HTTPException(status_code=400, detail="reference image URL cannot target a private address")


async def download_input_image(client: httpx.AsyncClient, value: str) -> tuple[bytes, str]:
    await validate_input_url(value)
    chunks: list[bytes] = []
    total = 0
    async with client.stream("GET", value, follow_redirects=False) as response:
        response.raise_for_status()
        if 300 <= response.status_code < 400:
            raise HTTPException(status_code=400, detail="reference image redirects are not accepted")
        mime_type = response.headers.get("content-type", "image/png").split(";", 1)[0]
        if not mime_type.startswith("image/"):
            raise HTTPException(status_code=400, detail="reference image URL did not return an image")
        async for chunk in response.aiter_bytes():
            total += len(chunk)
            if total > 30 * 1024 * 1024:
                raise HTTPException(status_code=413, detail="reference image exceeds 30 MB")
            chunks.append(chunk)
    return b"".join(chunks), mime_type


def encode_job(provider_id: str, prompt_id: str) -> str:
    return base64.urlsafe_b64encode(f"{provider_id}:{prompt_id}".encode()).decode().rstrip("=")


def decode_job(job_id: str) -> tuple[Provider, str]:
    try:
        raw = base64.urlsafe_b64decode(job_id + "=" * (-len(job_id) % 4)).decode()
        provider_id, prompt_id = raw.split(":", 1)
        provider = providers()[provider_id]
    except Exception as exc:
        raise HTTPException(status_code=404, detail="job not found") from exc
    return provider, prompt_id


@app.get("/health")
async def health() -> dict[str, Any]:
    return {"status": "ok", "providers": len(providers()), "workflows": len(load_registry())}


@app.get("/v1/providers", dependencies=[Depends(require_token)])
async def list_providers() -> dict[str, Any]:
    return {"providers": [{"id": item.id, "name": item.name, "baseUrl": item.base_url} for item in providers().values()]}


@app.get("/v1/workflows", dependencies=[Depends(require_token)])
async def list_workflows() -> dict[str, Any]:
    return {"workflows": [public_workflow(item) for item in load_registry().values()]}


@app.get("/v1/models", dependencies=[Depends(require_token)])
async def list_models() -> dict[str, Any]:
    return {"object": "list", "data": [public_model(item) for item in load_registry().values()]}


async def upload_image(client: httpx.AsyncClient, provider: Provider, value: str, index: int) -> str:
    value = value.strip()
    if value.startswith("comfy://"):
        return value.removeprefix("comfy://")
    if value.startswith("data:"):
        header, encoded = value.split(",", 1)
        mime_type = header.split(";", 1)[0].removeprefix("data:") or "image/png"
        if not mime_type.startswith("image/"):
            raise HTTPException(status_code=400, detail="reference data URL must contain an image")
        try:
            data = base64.b64decode(encoded, validate=True)
        except (ValueError, base64.binascii.Error) as exc:
            raise HTTPException(status_code=400, detail="reference image data URL is invalid") from exc
    elif value.startswith(("http://", "https://")):
        data, mime_type = await download_input_image(client, value)
    else:
        raise HTTPException(status_code=400, detail="input_images must contain http(s), data, or comfy URLs")
    if len(data) > 30 * 1024 * 1024:
        raise HTTPException(status_code=413, detail="reference image exceeds 30 MB")
    extension = mimetypes.guess_extension(mime_type) or ".png"
    filename = f"canvas-reference-{index}{extension}"
    response = await client.post(f"{provider.base_url}/upload/image", files={"image": (filename, data, mime_type)}, data={"overwrite": "true"}, headers=provider_headers(provider))
    response.raise_for_status()
    payload = response.json()
    return str(payload.get("name") or filename)


@app.post("/v1/jobs", dependencies=[Depends(require_token)])
async def create_job(request: JobRequest) -> dict[str, Any]:
    registry = load_registry()
    spec = registry.get(request.workflow_key)
    provider = providers().get(request.provider_id)
    if spec is None:
        raise HTTPException(status_code=400, detail="workflow is not registered")
    if provider is None:
        raise HTTPException(status_code=400, detail="provider is not configured")
    client_id = f"canvas-{secrets.token_hex(8)}"
    async with httpx.AsyncClient(timeout=60) as client:
        uploaded = [await upload_image(client, provider, value, index) for index, value in enumerate(request.input_images, start=1)]
        try:
            workflow = compile_workflow(spec, request.model_dump(), uploaded, client_id)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        response = await client.post(f"{provider.base_url}/prompt", json={"prompt": workflow, "client_id": client_id}, headers=provider_headers(provider))
        response.raise_for_status()
        payload = response.json()
    prompt_id = str(payload.get("prompt_id") or "").strip()
    if not prompt_id:
        raise HTTPException(status_code=502, detail="ComfyUI did not return prompt_id")
    return {"id": encode_job(provider.id, prompt_id), "promptId": prompt_id, "providerId": provider.id, "workflowKey": spec.key, "workflowRevision": spec.revision, "status": "submitted"}


def extract_outputs(entry: dict[str, Any], job_id: str) -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    for node in (entry.get("outputs") or {}).values():
        if not isinstance(node, dict):
            continue
        for field in ("images", "gifs", "audio"):
            for item in node.get(field) or []:
                if not isinstance(item, dict) or not item.get("filename"):
                    continue
                filename = str(item["filename"])
                mime_type = mimetypes.guess_type(filename)[0] or "application/octet-stream"
                kind = "video" if mime_type.startswith("video/") or filename.lower().endswith((".gif", ".webp")) and field == "gifs" else "audio" if mime_type.startswith("audio/") else "image"
                index = len(result)
                result.append({"index": index, "kind": kind, "mimeType": mime_type, "filename": filename, "url": f"/jobs/{quote(job_id, safe='')}/outputs/{index}"})
    return result


async def job_state(job_id: str) -> tuple[Provider, str, dict[str, Any], list[dict[str, Any]]]:
    provider, prompt_id = decode_job(job_id)
    async with httpx.AsyncClient(timeout=30) as client:
        response = await client.get(f"{provider.base_url}/history/{quote(prompt_id, safe='')}", headers=provider_headers(provider))
        response.raise_for_status()
        history = response.json()
    entry = history.get(prompt_id) or {}
    outputs = extract_outputs(entry, job_id)
    return provider, prompt_id, entry, outputs


@app.get("/v1/jobs/{job_id}", dependencies=[Depends(require_token)])
async def get_job(job_id: str) -> dict[str, Any]:
    provider, prompt_id, entry, outputs = await job_state(job_id)
    status_payload = entry.get("status") or {}
    if status_payload.get("status_str") == "error":
        status = "failed"
    elif outputs:
        status = "succeeded"
    elif status_payload.get("completed"):
        status = "failed"
    else:
        status = "running"
    return {"id": job_id, "promptId": prompt_id, "providerId": provider.id, "status": status, "outputs": outputs, "error": "ComfyUI execution failed" if status == "failed" else ""}


@app.post("/v1/jobs/{job_id}/cancel", dependencies=[Depends(require_token)])
async def cancel_job(job_id: str) -> dict[str, Any]:
    provider, prompt_id = decode_job(job_id)
    async with httpx.AsyncClient(timeout=30) as client:
        queue = (await client.get(f"{provider.base_url}/queue", headers=provider_headers(provider))).json()
        running = {str(item[1]) for item in queue.get("queue_running") or [] if isinstance(item, list) and len(item) > 1}
        if prompt_id in running:
            await client.post(f"{provider.base_url}/interrupt", json={}, headers=provider_headers(provider))
        await client.post(f"{provider.base_url}/queue", json={"delete": [prompt_id]}, headers=provider_headers(provider))
    return {"id": job_id, "status": "cancelled"}


@app.get("/v1/jobs/{job_id}/outputs/{index}", dependencies=[Depends(require_token)])
async def get_output(job_id: str, index: int) -> Response:
    provider, _, entry, outputs = await job_state(job_id)
    if index < 0 or index >= len(outputs):
        raise HTTPException(status_code=404, detail="output not found")
    wanted = outputs[index]
    locator: dict[str, Any] | None = None
    for node in (entry.get("outputs") or {}).values():
        if not isinstance(node, dict):
            continue
        for field in ("images", "gifs", "audio"):
            for item in node.get(field) or []:
                if isinstance(item, dict) and str(item.get("filename")) == wanted["filename"]:
                    locator = item
                    break
            if locator: break
        if locator: break
    if locator is None:
        raise HTTPException(status_code=404, detail="output locator not found")
    query = urlencode({"filename": locator.get("filename", ""), "subfolder": locator.get("subfolder", ""), "type": locator.get("type", "output")})
    client = httpx.AsyncClient(timeout=120)
    response = await client.send(client.build_request("GET", f"{provider.base_url}/view?{query}", headers=provider_headers(provider)), stream=True)
    if response.status_code >= 400:
        await response.aclose(); await client.aclose()
        raise HTTPException(status_code=502, detail="failed to read ComfyUI output")
    async def stream():
        try:
            async for chunk in response.aiter_bytes():
                yield chunk
        finally:
            await response.aclose(); await client.aclose()
    return StreamingResponse(stream(), media_type=response.headers.get("content-type", wanted["mimeType"]))

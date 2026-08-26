from __future__ import annotations

import copy
import hashlib
import json
import math
import random
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any


WORKFLOW_DIR = Path(__file__).resolve().parent / "workflows"
CAPABILITY_BY_MODE = {"t2i": "image", "i2i": "image", "text": "video", "image": "video"}
VISIBLE_TEXT_CONFLICTS = {
    "text", "words", "readable words", "readable text", "letters", "digits", "numbers",
    "chinese text", "chinese characters", "random text", "garbled text", "gibberish text",
    "pseudo-text", "pseudo-chinese glyphs", "malformed han characters", "mojibake",
    "corrupted glyphs", "labels", "signage", "typography", "text overlay", "logo",
    "brand logo", "brand name", "trademark", "printed words", "printed numbers",
}
H3_DIALOGUE_PATTERNS = (
    re.compile(r"<d>\[(?P<language>[^\]]+)]\s*(?P<text>.*?)</d>", re.IGNORECASE | re.DOTALL),
    re.compile(r"(?:口播|台词|对白|旁白|说(?:道)?|讲(?:道)?)[：:]\s*[\"“](?P<text>[^\"”]+)[\"”]"),
    re.compile(r"(?:says?|speaks?|narrates?)[：:\s]+[\"“](?P<text>[^\"”]+)[\"”]", re.IGNORECASE),
)


@dataclass(frozen=True)
class WorkflowSpec:
    key: str
    name: str
    mode: str
    file: str
    raw: dict[str, Any]
    revision: str

    @property
    def capability(self) -> str:
        return CAPABILITY_BY_MODE[self.mode]

    @property
    def path(self) -> Path:
        return WORKFLOW_DIR / self.file


def load_registry() -> dict[str, WorkflowSpec]:
    manifest = json.loads((WORKFLOW_DIR / "manifest.json").read_text(encoding="utf-8"))
    result: dict[str, WorkflowSpec] = {}
    for row in manifest.get("workflows") or []:
        if not row.get("enabled", True):
            continue
        mode = str(row.get("mode") or "")
        if mode not in CAPABILITY_BY_MODE:
            raise ValueError(f"unsupported workflow mode: {mode}")
        path = WORKFLOW_DIR / str(row["file"])
        payload = path.read_bytes()
        revision_material = payload + b"\0" + str(row.get("revision_salt") or "").encode("utf-8")
        key = str(row["key"])
        result[key] = WorkflowSpec(
            key=key,
            name=str(row.get("name") or key),
            mode=mode,
            file=str(row["file"]),
            raw=dict(row),
            revision=hashlib.sha256(revision_material).hexdigest()[:16],
        )
    return result


def public_workflow(spec: WorkflowSpec) -> dict[str, Any]:
    return {
        "key": spec.key,
        "name": spec.name,
        "mode": spec.mode,
        "capability": spec.capability,
        "revision": spec.revision,
        "supportsReferences": bool(spec.raw.get("load_image_node") or spec.raw.get("reference_node")),
    }


def public_model(spec: WorkflowSpec) -> dict[str, Any]:
    supports_references = bool(spec.raw.get("load_image_node") or spec.raw.get("reference_node"))
    if spec.mode == "t2i":
        operations = ["text_to_image"]
        min_images, max_images = 0, 0
    elif spec.mode == "i2i":
        operations = ["image_to_image"]
        min_images, max_images = 1, 1
    elif spec.mode == "text":
        operations = ["text_to_video"]
        if supports_references:
            operations.append("image_to_video")
        min_images, max_images = 0, 1 if supports_references else 0
    else:
        operations = ["image_to_video"]
        if spec.raw.get("reference_node"):
            operations.append("reference_to_video")
            min_images, max_images = 1, 9
        else:
            min_images, max_images = 1, 1

    aspect_ratios = ["1:1", "3:2", "2:3", "4:3", "3:4", "16:9", "9:16", "21:9"]
    endpoint_types = ["comfyui-workflow", *operations]
    item: dict[str, Any] = {
        "id": spec.key,
        "object": "model",
        "owned_by": "comfyui-workflow",
        "display_name": spec.name,
        "model_type": spec.capability,
        "supported_endpoint_types": endpoint_types,
        "supports_images": supports_references,
        "min_images": min_images,
        "max_images": max_images,
        "default_parameters": {"aspect_ratio": "1:1" if spec.capability == "image" else "16:9"},
        "options": {
            "aspect_ratio": [{"value": value, "label": value} for value in aspect_ratios],
        },
    }
    if spec.capability == "video":
        item["default_parameters"].update({"duration_seconds": "5", "resolution": "720p"})
        item["options"].update({
            "duration_seconds": [{"value": str(value), "label": f"{value} 秒"} for value in range(1, 31)],
            "resolution": [{"value": value, "label": value.upper()} for value in ("480p", "720p", "1080p")],
        })
    return item


def _patch_node(workflow: dict[str, Any], node_id: str | None, values: dict[str, Any]) -> None:
    if not node_id:
        return
    if node_id not in workflow:
        raise ValueError(f"workflow is missing configured node {node_id}")
    workflow[node_id].setdefault("inputs", {}).update(values)


def _fit_aligned_resolution(
    width: int,
    height: int,
    *,
    max_megapixels: float | None,
    align: int,
) -> tuple[int, int]:
    """Preserve aspect ratio while fitting the first pass to the workflow budget."""
    align = max(1, int(align))
    target_width = max(align, int(width))
    target_height = max(align, int(height))
    if max_megapixels is not None and float(max_megapixels) > 0:
        max_pixels = float(max_megapixels) * 1024 * 1024
        area = target_width * target_height
        if area > max_pixels:
            scale = math.sqrt(max_pixels / area)
            target_width = max(align, round(target_width * scale))
            target_height = max(align, round(target_height * scale))

    target_width = max(align, round(target_width / align) * align)
    target_height = max(align, round(target_height / align) * align)
    if max_megapixels is not None and float(max_megapixels) > 0:
        max_pixels = float(max_megapixels) * 1024 * 1024
        while target_width * target_height > max_pixels:
            if target_width >= target_height and target_width > align:
                target_width -= align
            elif target_height > align:
                target_height -= align
            else:
                break
    return target_width, target_height


def _negative_prompt(spec: WorkflowSpec, current: str, requested: str) -> str:
    parts: list[str] = []
    seen: set[str] = set()
    for value in (current, requested):
        for raw in value.split(","):
            term = raw.strip(" .")
            normalized = " ".join(term.casefold().split())
            if not term or normalized in seen:
                continue
            if spec.raw.get("preserve_visible_text") and normalized in VISIBLE_TEXT_CONFLICTS:
                continue
            seen.add(normalized)
            parts.append(term)
    return ", ".join(parts)


def _spoken_language(value: str) -> str:
    if re.search(r"[\u3040-\u30ff]", value):
        return "Japanese"
    if re.search(r"[\uac00-\ud7af]", value):
        return "Korean"
    if re.search(r"[\u3400-\u9fff]", value):
        return "Chinese"
    return "English"


def _h3_audio_prompt(prompt: str, duration: float) -> str:
    dialogue: list[tuple[str, str]] = []
    seen: set[str] = set()
    for pattern in H3_DIALOGUE_PATTERNS:
        for match in pattern.finditer(prompt):
            text = " ".join(str(match.groupdict().get("text") or "").split()).strip()
            if not text or text in seen:
                continue
            seen.add(text)
            language = str(match.groupdict().get("language") or _spoken_language(text)).strip()
            dialogue.append((language, text))

    if dialogue:
        cjk_count = sum(len(re.findall(r"[\u3400-\u9fff]", text)) for _, text in dialogue)
        max_cjk_count = max(5, int(max(1.0, duration) * 5))
        if cjk_count > max_cjk_count:
            raise ValueError(
                f"Chinese dialogue is too long for {duration:g} seconds: {cjk_count} characters, maximum {max_cjk_count}"
            )
        exact_lines = " ".join(
            f"The on-screen speaker (S{index}) says exactly once: <d>[{language}] {text}</d>."
            for index, (language, text) in enumerate(dialogue, start=1)
        )
        audio_rule = (
            "H3 audio compliance: " + exact_lines + " "
            "Each speaker uses clear natural pronunciation in the declared language. "
            "Do not translate, paraphrase, replace, repeat, or invent any syllable. "
            "No other voice, narration, singing, chanting, or background conversation is audible."
        )
    else:
        audio_rule = (
            "H3 audio compliance: No character speaks. No dialogue, narration, singing, chanting, "
            "vocalization, background conversation, or invented language is audible. "
            "Generate environmental ambience and physical action sounds only."
        )
    return f"{prompt.rstrip()}\n\n{audio_rule}"


def compile_workflow(spec: WorkflowSpec, request: dict[str, Any], uploaded_images: list[str], job_id: str) -> dict[str, Any]:
    workflow = copy.deepcopy(json.loads(spec.path.read_text(encoding="utf-8")))
    prompt = str(request.get("prompt") or "").strip()
    if not prompt:
        raise ValueError("prompt is required")
    native_audio_control = bool(spec.raw.get("native_audio_control"))
    generate_audio = request.get("generate_audio")
    if native_audio_control and generate_audio is None:
        generate_audio = bool(spec.raw.get("native_audio_default", True))
    if native_audio_control and generate_audio and spec.raw.get("speech_prompt_policy") == "minimax_h3":
        prompt = _h3_audio_prompt(prompt, float(request.get("duration") or 5))
    negative_prompt = str(request.get("negative_prompt") or "").strip()
    seed = request.get("seed")
    if seed is None:
        seed = random.SystemRandom().randint(0, 2**63 - 1)

    _patch_node(workflow, spec.raw.get("prompt_node"), {str(spec.raw.get("prompt_input") or "text"): prompt})
    _patch_node(workflow, spec.raw.get("motion_node"), {str(spec.raw.get("motion_input") or "text"): prompt})
    _patch_node(workflow, spec.raw.get("seed_node"), {str(spec.raw.get("seed_input") or "noise_seed"): int(seed)})
    if negative_prompt and spec.raw.get("negative_node"):
        node_id = str(spec.raw["negative_node"])
        input_key = str(spec.raw.get("negative_input") or "text")
        current = str((workflow.get(node_id, {}).get("inputs") or {}).get(input_key) or "").strip()
        _patch_node(workflow, node_id, {input_key: _negative_prompt(spec, current, negative_prompt)})
    requested_width = request.get("width")
    requested_height = request.get("height")
    if requested_width is not None and requested_height is not None and spec.raw.get("base_max_megapixels") is not None:
        align = max(1, int(spec.raw.get("dimension_align") or 1))
        base_width, base_height = _fit_aligned_resolution(
            int(requested_width),
            int(requested_height),
            max_megapixels=float(spec.raw["base_max_megapixels"]),
            align=align,
        )
        _patch_node(workflow, spec.raw.get("width_node"), {str(spec.raw.get("width_input") or "value"): base_width})
        _patch_node(workflow, spec.raw.get("height_node"), {str(spec.raw.get("height_input") or "value"): base_height})
        if spec.raw.get("upscale_node"):
            final_width, final_height = _fit_aligned_resolution(
                int(requested_width),
                int(requested_height),
                max_megapixels=None,
                align=align,
            )
            upscale_values: dict[str, Any] = {
                str(spec.raw.get("upscale_mode_input") or "mode"): str(spec.raw.get("upscale_mode") or "target dimensions"),
                str(spec.raw.get("upscale_width_input") or "mode.width"): final_width,
                str(spec.raw.get("upscale_height_input") or "mode.height"): final_height,
            }
            chunking_input = spec.raw.get("upscale_chunking_input")
            threshold = spec.raw.get("upscale_chunking_threshold_sec")
            if chunking_input and threshold is not None:
                upscale_values[str(chunking_input)] = float(request.get("duration") or 0) > float(threshold)
            _patch_node(workflow, str(spec.raw["upscale_node"]), upscale_values)
    else:
        if requested_width is not None:
            _patch_node(workflow, spec.raw.get("width_node"), {str(spec.raw.get("width_input") or "value"): int(requested_width)})
        if requested_height is not None:
            _patch_node(workflow, spec.raw.get("height_node"), {str(spec.raw.get("height_input") or "value"): int(requested_height)})
    if request.get("duration") is not None:
        _patch_node(workflow, spec.raw.get("duration_node"), {"value": float(request["duration"])})
    count = max(1, min(int(request.get("batch_size") or 1), 4))
    if spec.raw.get("batch_node"):
        _patch_node(workflow, str(spec.raw["batch_node"]), {str(spec.raw.get("batch_input") or "batch_size"): count})
    elif count > 1 and spec.raw.get("batch_source_node") and spec.raw.get("batch_target_node"):
        source_node = str(spec.raw["batch_source_node"])
        target_node = str(spec.raw["batch_target_node"])
        if source_node not in workflow or target_node not in workflow:
            raise ValueError("workflow batch source or target node is missing")
        batch_node = "canvas_repeat_latent_batch"
        while batch_node in workflow:
            batch_node = "_" + batch_node
        workflow[batch_node] = {"class_type": "RepeatLatentBatch", "inputs": {"samples": [source_node, 0], "amount": count}, "_meta": {"title": "Canvas Batch Output"}}
        _patch_node(workflow, target_node, {str(spec.raw.get("batch_target_input") or "latent_image"): [batch_node, 0]})
    if spec.raw.get("enhance_node") is not None and spec.raw.get("enhance_default") is not None:
        _patch_node(workflow, str(spec.raw["enhance_node"]), {str(spec.raw.get("enhance_input") or "value"): bool(spec.raw["enhance_default"])})
    if spec.raw.get("denoise_node") and (request.get("denoise") is not None or spec.raw.get("denoise_default") is not None):
        denoise = request.get("denoise") if request.get("denoise") is not None else spec.raw.get("denoise_default")
        _patch_node(workflow, str(spec.raw["denoise_node"]), {str(spec.raw.get("denoise_input") or "denoise"): float(denoise)})
    if spec.raw.get("max_dimension_node") and (request.get("width") is not None or request.get("height") is not None):
        maximum = max(int(request.get("width") or 0), int(request.get("height") or 0), 64)
        _patch_node(workflow, str(spec.raw["max_dimension_node"]), {str(spec.raw.get("max_dimension_input") or "largest_size"): maximum})

    if spec.raw.get("reference_node"):
        if not uploaded_images:
            raise ValueError("this workflow requires at least one reference image")
        max_reference_images = int(spec.raw.get("max_reference_images") or 9)
        if len(uploaded_images) > max_reference_images:
            raise ValueError(f"this workflow supports at most {max_reference_images} reference images")
        reference_inputs: dict[str, Any] = {}
        prefix = str(spec.raw.get("reference_input_prefix") or "ref_images.ref_image_")
        for index, image_name in enumerate(uploaded_images):
            node_id = f"canvas_reference_{index + 1}"
            while node_id in workflow:
                node_id = "_" + node_id
            workflow[node_id] = {"class_type": "LoadImage", "inputs": {"image": image_name}, "_meta": {"title": f"Canvas reference {index + 1}"}}
            reference_inputs[f"{prefix}{index}"] = [node_id, 0]
        _patch_node(workflow, str(spec.raw["reference_node"]), reference_inputs)
    elif spec.raw.get("load_image_node"):
        if spec.mode in {"i2i", "image"} and not uploaded_images:
            raise ValueError("this workflow requires an input image")
        if uploaded_images:
            load_node = str(spec.raw["load_image_node"])
            if load_node not in workflow and spec.mode == "text":
                workflow[load_node] = {"class_type": "LoadImage", "inputs": {"image": uploaded_images[0]}, "_meta": {"title": "Canvas Reference Image"}}
            else:
                _patch_node(workflow, load_node, {"image": uploaded_images[0]})
            if spec.raw.get("image_resize_node"):
                resize_node = str(spec.raw["image_resize_node"])
                if resize_node in workflow:
                    _patch_node(workflow, resize_node, {"input": [load_node, 0]})
            if spec.raw.get("t2v_switch_node"):
                _patch_node(workflow, str(spec.raw["t2v_switch_node"]), {"value": False})
        elif spec.raw.get("t2v_switch_node"):
            _patch_node(workflow, str(spec.raw["t2v_switch_node"]), {"value": True})

    save_node = spec.raw.get("save_node")
    if save_node:
        prefix = str(spec.raw.get("save_prefix") or spec.key)
        _patch_node(workflow, str(save_node), {"filename_prefix": f"{prefix}_{job_id}"})
        if native_audio_control and not generate_audio:
            workflow[str(save_node)].setdefault("inputs", {}).pop("audio", None)
    return workflow

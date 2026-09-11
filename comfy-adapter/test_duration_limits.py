import unittest
from unittest.mock import AsyncMock, patch

from pydantic import ValidationError
import app
from workflow import compile_workflow, load_registry, public_model


class VideoDurationTests(unittest.TestCase):
    def test_all_video_models_advertise_at_most_fifteen_seconds(self):
        for spec in load_registry().values():
            if spec.capability != "video":
                continue
            values = [int(item["value"]) for item in public_model(spec)["options"]["duration_seconds"]]
            self.assertEqual(values, list(range(1, 16)))

    def test_fifteen_seconds_keeps_requested_hd_resolution(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_i2v"]
        for width, height in [(1920, 1088), (1088, 1920)]:
            result = compile_workflow(spec, {"prompt": "A landscape.", "duration": 15, "width": width, "height": height}, ["reference.png"], "duration-test")
            self.assertEqual(result["132"]["inputs"]["value"], 15)
            self.assertEqual(result["415"]["inputs"]["mode.width"], width)
            self.assertEqual(result["415"]["inputs"]["mode.height"], height)

    def test_invalid_duration_is_rejected_before_upload_or_submission(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_i2v"]
        for value in [0, -1, 15.1, 16, 30, float("inf"), float("nan")]:
            with self.subTest(duration=value):
                with self.assertRaises(ValidationError):
                    app.JobRequest(workflow_key=spec.key, prompt="test", duration=value)
                with self.assertRaisesRegex(ValueError, "最多 15 秒"):
                    compile_workflow(spec, {"prompt": "test", "duration": value}, [], "invalid")
        self.assertEqual(app.JobRequest(workflow_key=spec.key, prompt="test", duration=15).duration, 15)


class JobErrorTests(unittest.IsolatedAsyncioTestCase):
    async def test_stride_error_has_actionable_message_without_private_inputs(self):
        provider = app.Provider("gpu3", "GPU3", "http://fixture")
        entry = {"status": {"status_str": "error", "messages": [["execution_error", {"exception_message": "tensor strides exceed int32 range", "current_inputs": "private prompt"}]]}}
        with patch.object(app, "job_state", new=AsyncMock(return_value=(provider, "id", entry, []))):
            result = await app.get_job("fixture")
        self.assertEqual(result["status"], "failed")
        self.assertIn("最多 15 秒", result["error"])
        self.assertNotIn("private", str(result))

    async def test_unknown_errors_keep_generic_message(self):
        provider = app.Provider("gpu3", "GPU3", "http://fixture")
        entry = {"status": {"status_str": "error", "messages": [None, [], ["execution_error", "private input"]]}}
        with patch.object(app, "job_state", new=AsyncMock(return_value=(provider, "id", entry, []))):
            result = await app.get_job("fixture")
        self.assertEqual(result["error"], "ComfyUI execution failed")


if __name__ == "__main__":
    unittest.main()

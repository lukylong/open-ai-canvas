import json
import os
import unittest
from unittest.mock import AsyncMock, patch

import httpx

import app


PROVIDERS = [
    {"id": "default", "name": "GPU2", "base_url": "http://comfyui:8188"},
    {"id": "gpu3", "name": "GPU3", "base_url": "http://comfyui-gpu3:8188"},
]


class ProviderSelectionTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.environment = patch.dict(
            os.environ,
            {
                "COMFY_PROVIDERS_JSON": json.dumps(PROVIDERS),
                "COMFY_AUTO_BALANCE": "true",
            },
            clear=False,
        )
        self.environment.start()
        app.provider_reservations.clear()

    def tearDown(self):
        self.environment.stop()

    async def test_default_request_uses_provider_with_shorter_queue(self):
        depths = {"default": 14, "gpu3": 0}

        async def queue_depth(provider):
            return depths[provider.id]

        with patch.object(app, "provider_queue_depth", side_effect=queue_depth):
            selected = await app.select_provider("default")

        self.assertIsNotNone(selected)
        self.assertEqual(selected.id, "gpu3")

    async def test_equal_queue_depth_keeps_default_provider(self):
        with patch.object(app, "provider_queue_depth", new=AsyncMock(return_value=0)):
            selected = await app.select_provider("default")

        self.assertIsNotNone(selected)
        self.assertEqual(selected.id, "default")

    async def test_explicit_provider_bypasses_auto_balance(self):
        probe = AsyncMock(return_value=0)
        with patch.object(app, "provider_queue_depth", new=probe):
            selected = await app.select_provider("gpu3")

        self.assertIsNotNone(selected)
        self.assertEqual(selected.id, "gpu3")
        probe.assert_not_awaited()

    async def test_unreachable_secondary_provider_falls_back_to_default(self):
        depths = {"default": 4, "gpu3": None}

        async def queue_depth(provider):
            return depths[provider.id]

        with patch.object(app, "provider_queue_depth", side_effect=queue_depth):
            selected = await app.select_provider("default")

        self.assertIsNotNone(selected)
        self.assertEqual(selected.id, "default")

    async def test_inflight_reservation_breaks_a_concurrent_queue_tie(self):
        with patch.object(app, "provider_queue_depth", new=AsyncMock(return_value=0)):
            first = await app.reserve_provider("default")
            second = await app.reserve_provider("default")

        self.assertIsNotNone(first)
        self.assertIsNotNone(second)
        self.assertEqual(first.id, "default")
        self.assertEqual(second.id, "gpu3")
        await app.release_provider(first)
        await app.release_provider(second)
        self.assertEqual(app.provider_reservations, {})

    async def test_history_timeout_is_reported_as_running(self):
        job_id = app.encode_job("default", "prompt-1")
        with patch.object(app, "job_state", new=AsyncMock(side_effect=httpx.ReadTimeout("busy"))):
            result = await app.get_job(job_id)

        self.assertEqual(result["status"], "running")
        self.assertTrue(result["pollDeferred"])
        self.assertEqual(result["providerId"], "default")


if __name__ == "__main__":
    unittest.main()

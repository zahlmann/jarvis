"""Voice handling: transcription with OpenAI and TTS with ElevenLabs."""

import os
import tempfile
from pathlib import Path

from openai import AsyncOpenAI
from elevenlabs import AsyncElevenLabs


class VoiceHandler:
    """Handle voice transcription and text-to-speech."""

    def __init__(self):
        self.openai = AsyncOpenAI(api_key=os.environ["OPENAI_API_KEY"])
        self.elevenlabs = AsyncElevenLabs(api_key=os.environ["ELEVENLABS_API_KEY"])
        self.voice_id = os.environ.get("ELEVENLABS_VOICE_ID", "EkK5I93UQWFDigLMpZcX")

    async def transcribe(self, audio_data: bytes, content_type: str = "audio/ogg") -> str:
        """Transcribe audio using OpenAI gpt-4o-transcribe."""
        # Determine file extension from content type
        ext_map = {
            "audio/ogg": ".ogg",
            "audio/mpeg": ".mp3",
            "audio/mp4": ".m4a",
            "audio/wav": ".wav",
            "audio/webm": ".webm",
        }
        ext = ext_map.get(content_type, ".ogg")

        # Write to temp file (OpenAI API needs a file)
        with tempfile.NamedTemporaryFile(suffix=ext, delete=False) as f:
            f.write(audio_data)
            temp_path = f.name

        try:
            with open(temp_path, "rb") as audio_file:
                response = await self.openai.audio.transcriptions.create(
                    model="gpt-4o-transcribe",
                    file=audio_file,
                )
            return response.text
        finally:
            Path(temp_path).unlink(missing_ok=True)

    async def text_to_speech(self, text: str) -> tuple[bytes, str]:
        """
        Convert text to speech using ElevenLabs v3.

        Text can include audio tags like [excited], [laughs], [whispers].
        Returns (audio_bytes, file_path) where file_path is a temp file.
        """
        # Use ElevenLabs text-to-speech
        audio_generator = self.elevenlabs.text_to_speech.convert(
            voice_id=self.voice_id,
            text=text,
            model_id="eleven_v3",
            output_format="mp3_44100_128",
        )

        # Collect all audio chunks
        audio_chunks = []
        async for chunk in audio_generator:
            audio_chunks.append(chunk)
        audio_data = b"".join(audio_chunks)

        # Save to temp file for WhatsApp upload
        with tempfile.NamedTemporaryFile(suffix=".mp3", delete=False) as f:
            f.write(audio_data)
            temp_path = f.name

        return audio_data, temp_path

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setTokens, captureVoice, ApiError } from './client';

describe('captureVoice', () => {
  beforeEach(() => {
    setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    vi.restoreAllMocks();
  });

  it('sends multipart form with audio blob', async () => {
    const mockNote = {
      id: 'voice1',
      title: 'Voice Note',
      body: 'Transcribed text',
      file_path: 'inbox/voice-note.md',
      transcript_source: true,
      tags: [],
      created_at: '2026-03-08T10:00:00Z',
      updated_at: '2026-03-08T10:00:00Z',
    };

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockNote), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const audioBlob = new Blob(['fake-audio-data'], { type: 'audio/webm' });
    const result = await captureVoice(audioBlob, 'recording.webm');

    expect(result.id).toBe('voice1');
    expect(result.transcript_source).toBe(true);

    // Verify fetch was called with FormData body
    expect(fetchSpy).toHaveBeenCalledOnce();
    const [, options] = fetchSpy.mock.calls[0];
    expect(options?.body).toBeInstanceOf(FormData);

    const formData = options?.body as FormData;
    const audioFile = formData.get('audio') as File;
    expect(audioFile).toBeTruthy();
    expect(audioFile.name).toBe('recording.webm');
  });

  it('includes Authorization header', async () => {
    const mockNote = {
      id: 'voice2',
      title: 'Voice Note 2',
      body: 'More text',
      file_path: 'inbox/voice-note-2.md',
      tags: [],
      created_at: '2026-03-08T10:00:00Z',
      updated_at: '2026-03-08T10:00:00Z',
    };

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockNote), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const audioBlob = new Blob(['audio'], { type: 'audio/webm' });
    await captureVoice(audioBlob);

    const [, options] = fetchSpy.mock.calls[0];
    const headers = options?.headers as Record<string, string>;
    expect(headers['Authorization']).toBe('Bearer valid_token');
  });

  it('retries on 401', async () => {
    const mockNote = {
      id: 'voice3',
      title: 'Retried Note',
      body: 'Transcribed after retry',
      file_path: 'inbox/retried.md',
      tags: [],
      created_at: '2026-03-08T10:00:00Z',
      updated_at: '2026-03-08T10:00:00Z',
    };

    const refreshTokens = { access_token: 'new_at', refresh_token: 'rt' };

    vi.spyOn(globalThis, 'fetch')
      // First call: 401 (expired token)
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      // Refresh call: succeeds with new tokens
      .mockResolvedValueOnce(
        new Response(JSON.stringify(refreshTokens), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      // Retry call: succeeds
      .mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

    const audioBlob = new Blob(['audio'], { type: 'audio/webm' });
    const result = await captureVoice(audioBlob, 'recording.webm');

    expect(result.id).toBe('voice3');
    expect(result.title).toBe('Retried Note');
  });

  it('throws ApiError on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'transcription failed' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const audioBlob = new Blob(['audio'], { type: 'audio/webm' });
    await expect(captureVoice(audioBlob)).rejects.toThrow(ApiError);

    try {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'bad request' }), {
          status: 400,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
      await captureVoice(audioBlob);
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(400);
    }
  });
});

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CaptureModal } from './CaptureModal';

// Mock stores
const mockSetCaptureModalOpen = vi.fn();
const mockCreateNote = vi.fn();
const mockNavigate = vi.fn();

vi.mock('../../stores/uiStore', () => ({
  useUIStore: vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
    const state: Record<string, unknown> = {
      captureModalOpen: true,
      setCaptureModalOpen: mockSetCaptureModalOpen,
    };
    return selector(state);
  }),
}));

vi.mock('../../stores/noteStore', () => ({
  useNoteStore: vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
    const state: Record<string, unknown> = {
      createNote: mockCreateNote,
    };
    return selector(state);
  }),
}));

vi.mock('../../stores/projectStore', () => ({
  useProjectStore: vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
    const state: Record<string, unknown> = {
      projects: [],
    };
    return selector(state);
  }),
}));

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock('../../api/client', () => ({
  captureURL: vi.fn(),
  captureVoice: vi.fn(),
  listTemplates: vi.fn().mockResolvedValue([]),
  applyTemplate: vi.fn(),
}));

// Access the mocked modules for per-test overrides
import { useUIStore } from '../../stores/uiStore';
import { listTemplates } from '../../api/client';

function renderModal() {
  return render(
    <MemoryRouter>
      <CaptureModal />
    </MemoryRouter>,
  );
}

describe('CaptureModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset to default open state
    vi.mocked(useUIStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) => {
        const state: Record<string, unknown> = {
          captureModalOpen: true,
          setCaptureModalOpen: mockSetCaptureModalOpen,
        };
        return selector(state);
      },
    );
    vi.mocked(listTemplates).mockResolvedValue([]);
  });

  it('renders when open', () => {
    renderModal();
    expect(screen.getByText('Quick Capture')).toBeInTheDocument();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('does not render when closed', () => {
    vi.mocked(useUIStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) => {
        const state: Record<string, unknown> = {
          captureModalOpen: false,
          setCaptureModalOpen: mockSetCaptureModalOpen,
        };
        return selector(state);
      },
    );

    const { container } = renderModal();
    expect(container.innerHTML).toBe('');
  });

  it('shows URL mode banner when URL detected', async () => {
    renderModal();

    const textarea = screen.getByPlaceholderText('Write your thought...');
    fireEvent.change(textarea, {
      target: { value: 'https://example.com' },
    });

    await waitFor(() => {
      expect(
        screen.getByText('URL detected - will fetch and save page content'),
      ).toBeInTheDocument();
    });
  });

  it('shows template picker when templates available', async () => {
    vi.mocked(listTemplates).mockResolvedValue([
      { name: 'meeting-notes', description: 'Meeting notes template' },
      { name: 'daily-log', description: 'Daily log template' },
    ]);

    renderModal();

    await waitFor(() => {
      expect(screen.getByText('Use a template')).toBeInTheDocument();
    });
  });

  it('calls close on Cancel button click', () => {
    renderModal();

    const cancelButton = screen.getByText('Cancel');
    fireEvent.click(cancelButton);

    expect(mockSetCaptureModalOpen).toHaveBeenCalledWith(false);
  });

  it('disables save when empty', () => {
    renderModal();

    const saveButton = screen.getByText('Save to Inbox');
    expect(saveButton).toBeDisabled();
  });
});

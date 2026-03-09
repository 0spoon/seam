import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SynthesisModal } from './SynthesisModal';

vi.mock('../../api/client', () => ({
  synthesize: vi.fn(),
}));

vi.mock('../../lib/markdown', () => ({
  renderMarkdown: (s: string) => `<p>${s}</p>`,
}));

import { synthesize } from '../../api/client';

const defaultProps = {
  scope: 'tag' as const,
  tag: 'architecture',
  title: 'Synthesize: architecture',
  onClose: vi.fn(),
};

describe('SynthesisModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders with title', () => {
    render(<SynthesisModal {...defaultProps} />);
    expect(screen.getByText('Synthesize: architecture')).toBeInTheDocument();
  });

  it('has default prompt value', () => {
    render(<SynthesisModal {...defaultProps} />);
    const input = screen.getByPlaceholderText('What would you like to know?') as HTMLInputElement;
    expect(input.value).toBe('Summarize the key themes and ideas');
  });

  it('calls onClose when close button is clicked', () => {
    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByLabelText('Close'));
    expect(defaultProps.onClose).toHaveBeenCalledTimes(1);
  });

  it('calls onClose when backdrop is clicked', () => {
    const { container } = render(<SynthesisModal {...defaultProps} />);
    // The backdrop is the outermost div.
    const backdrop = container.firstChild as HTMLElement;
    fireEvent.click(backdrop);
    expect(defaultProps.onClose).toHaveBeenCalledTimes(1);
  });

  it('does not call onClose when modal body is clicked', () => {
    render(<SynthesisModal {...defaultProps} />);
    const title = screen.getByText('Synthesize: architecture');
    fireEvent.click(title);
    expect(defaultProps.onClose).not.toHaveBeenCalled();
  });

  it('calls synthesize with correct params on Generate click', async () => {
    vi.mocked(synthesize).mockResolvedValueOnce({
      response: 'Key themes: caching and APIs',
    });

    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(synthesize).toHaveBeenCalledWith({
        scope: 'tag',
        tag: 'architecture',
        prompt: 'Summarize the key themes and ideas',
      });
    });
  });

  it('calls synthesize with project scope', async () => {
    vi.mocked(synthesize).mockResolvedValueOnce({
      response: 'Summary',
    });

    render(
      <SynthesisModal
        scope="project"
        projectId="proj1"
        title="Synthesize: My Project"
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(synthesize).toHaveBeenCalledWith({
        scope: 'project',
        project_id: 'proj1',
        prompt: 'Summarize the key themes and ideas',
      });
    });
  });

  it('displays synthesis response rendered as markdown', async () => {
    vi.mocked(synthesize).mockResolvedValueOnce({
      response: 'Key themes: caching and APIs',
    });

    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      // renderMarkdown mock wraps in <p> tags.
      expect(screen.getByText('Key themes: caching and APIs')).toBeInTheDocument();
    });
  });

  it('shows loading state while synthesizing', async () => {
    let resolvePromise!: (v: { response: string }) => void;
    vi.mocked(synthesize).mockReturnValueOnce(
      new Promise((resolve) => {
        resolvePromise = resolve;
      }),
    );

    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(screen.getByText('Synthesizing your notes...')).toBeInTheDocument();
    });

    // Prompt input should be disabled during loading.
    const input = screen.getByPlaceholderText('What would you like to know?');
    expect(input).toBeDisabled();

    // Resolve to clean up.
    resolvePromise({ response: 'done' });
    await waitFor(() => {
      expect(screen.queryByText('Synthesizing your notes...')).not.toBeInTheDocument();
    });
  });

  it('shows error message on synthesis failure', async () => {
    vi.mocked(synthesize).mockRejectedValueOnce(new Error('model unavailable'));

    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(screen.getByText('model unavailable')).toBeInTheDocument();
    });
  });

  it('shows generic error for non-Error exceptions', async () => {
    vi.mocked(synthesize).mockRejectedValueOnce('something broke');

    render(<SynthesisModal {...defaultProps} />);
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(screen.getByText('Synthesis failed')).toBeInTheDocument();
    });
  });

  it('triggers synthesize on Enter key', async () => {
    vi.mocked(synthesize).mockResolvedValueOnce({ response: 'done' });

    render(<SynthesisModal {...defaultProps} />);
    const input = screen.getByPlaceholderText('What would you like to know?');
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(synthesize).toHaveBeenCalled();
    });
  });

  it('does not synthesize with empty prompt', () => {
    render(<SynthesisModal {...defaultProps} />);
    const input = screen.getByPlaceholderText('What would you like to know?');
    fireEvent.change(input, { target: { value: '' } });

    // Generate button should be disabled.
    const generateButton = screen.getByText('Generate');
    expect(generateButton).toBeDisabled();
  });

  it('allows custom prompt text', async () => {
    vi.mocked(synthesize).mockResolvedValueOnce({ response: 'result' });

    render(<SynthesisModal {...defaultProps} />);
    const input = screen.getByPlaceholderText('What would you like to know?');
    fireEvent.change(input, { target: { value: 'List all action items' } });
    fireEvent.click(screen.getByText('Generate'));

    await waitFor(() => {
      expect(synthesize).toHaveBeenCalledWith(
        expect.objectContaining({ prompt: 'List all action items' }),
      );
    });
  });
});

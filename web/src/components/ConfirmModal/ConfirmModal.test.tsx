import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

vi.mock('motion/react', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: ({ children, className, onClick, onKeyDown, ...props }: any) => (
      <div
        className={className}
        onClick={onClick}
        onKeyDown={onKeyDown}
        {...Object.fromEntries(
          Object.entries(props).filter(
            ([k]) =>
              !['initial', 'animate', 'exit', 'transition', 'layout'].includes(
                k,
              ),
          ),
        )}
      >
        {children}
      </div>
    ),
  },
}));

import { ConfirmModal } from './ConfirmModal';

const defaultProps = {
  open: true,
  title: 'Delete note',
  message: 'This action cannot be undone.',
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
};

describe('ConfirmModal', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <ConfirmModal {...defaultProps} open={false} />,
    );
    expect(container.querySelector('[role="alertdialog"]')).toBeNull();
  });

  it('renders title and message when open', () => {
    render(<ConfirmModal {...defaultProps} />);
    expect(screen.getByText('Delete note')).toBeInTheDocument();
    expect(
      screen.getByText('This action cannot be undone.'),
    ).toBeInTheDocument();
  });

  it('renders custom confirm and cancel labels', () => {
    render(
      <ConfirmModal
        {...defaultProps}
        confirmLabel="Yes, delete"
        cancelLabel="Go back"
      />,
    );
    expect(screen.getByText('Yes, delete')).toBeInTheDocument();
    expect(screen.getByText('Go back')).toBeInTheDocument();
  });

  it('renders default labels when not provided', () => {
    render(<ConfirmModal {...defaultProps} />);
    expect(screen.getByText('Confirm')).toBeInTheDocument();
    expect(screen.getByText('Cancel')).toBeInTheDocument();
  });

  it('calls onConfirm when confirm button clicked', () => {
    const onConfirm = vi.fn();
    render(<ConfirmModal {...defaultProps} onConfirm={onConfirm} />);
    fireEvent.click(screen.getByText('Confirm'));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it('calls onCancel when cancel button clicked', () => {
    const onCancel = vi.fn();
    render(<ConfirmModal {...defaultProps} onCancel={onCancel} />);
    fireEvent.click(screen.getByText('Cancel'));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it('renders children content', () => {
    render(
      <ConfirmModal {...defaultProps}>
        <span>Extra details here</span>
      </ConfirmModal>,
    );
    expect(screen.getByText('Extra details here')).toBeInTheDocument();
  });

  it('applies destructive style class when destructive is true', () => {
    render(<ConfirmModal {...defaultProps} destructive />);
    const confirmButton = screen.getByText('Confirm');
    expect(confirmButton.className).toContain('destructiveButton');
  });
});

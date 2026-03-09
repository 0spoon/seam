import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';

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

import { useToastStore, ToastContainer } from './ToastContainer';

let uuidCounter = 0;

beforeEach(() => {
  vi.useFakeTimers();
  uuidCounter = 0;
  vi.stubGlobal('crypto', {
    randomUUID: () => `test-uuid-${++uuidCounter}`,
  });
  // Reset the store between tests
  useToastStore.setState({ toasts: [] });
});

describe('useToastStore', () => {
  it('addToast adds a toast with correct type', () => {
    useToastStore.getState().addToast('Something went wrong', 'error');
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(1);
    expect(toasts[0].message).toBe('Something went wrong');
    expect(toasts[0].type).toBe('error');
  });

  it('addToast defaults to info type', () => {
    useToastStore.getState().addToast('Saved');
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('info');
  });

  it('removeToast removes a toast by id', () => {
    useToastStore.getState().addToast('First');
    const { toasts } = useToastStore.getState();
    const id = toasts[0].id;
    useToastStore.getState().removeToast(id);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it('addToast limits to 3 toasts keeping last 2 plus new', () => {
    useToastStore.getState().addToast('First');
    useToastStore.getState().addToast('Second');
    useToastStore.getState().addToast('Third');
    expect(useToastStore.getState().toasts).toHaveLength(3);

    useToastStore.getState().addToast('Fourth');
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(3);
    expect(toasts[0].message).toBe('Second');
    expect(toasts[1].message).toBe('Third');
    expect(toasts[2].message).toBe('Fourth');
  });
});

describe('ToastContainer', () => {
  it('renders nothing when no toasts', () => {
    render(<ToastContainer />);
    const container = screen.getByRole('status');
    expect(container).toBeInTheDocument();
    expect(container.children).toHaveLength(0);
  });

  it('renders toast messages', () => {
    act(() => {
      useToastStore.getState().addToast('Hello world');
      useToastStore.getState().addToast('Second message', 'success');
    });
    render(<ToastContainer />);
    expect(screen.getByText('Hello world')).toBeInTheDocument();
    expect(screen.getByText('Second message')).toBeInTheDocument();
  });

  it('toast is clickable to dismiss', () => {
    act(() => {
      useToastStore.getState().addToast('Dismiss me');
    });
    render(<ToastContainer />);
    expect(screen.getByText('Dismiss me')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Dismiss me'));
    expect(screen.queryByText('Dismiss me')).not.toBeInTheDocument();
  });
});

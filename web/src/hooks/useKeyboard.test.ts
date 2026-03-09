import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useKeyboard } from './useKeyboard';

function dispatchKey(opts: KeyboardEventInit) {
  window.dispatchEvent(new KeyboardEvent('keydown', { ...opts, bubbles: true }));
}

describe('useKeyboard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('calls handler when matching key pressed', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'a', handler }]),
    );

    dispatchKey({ key: 'a' });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('does not call handler for non-matching key', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'a', handler }]),
    );

    dispatchKey({ key: 'b' });
    expect(handler).not.toHaveBeenCalled();
  });

  it('matches ctrl modifier', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'k', ctrl: true, handler }]),
    );

    // Without ctrl -- should not fire
    dispatchKey({ key: 'k' });
    expect(handler).not.toHaveBeenCalled();

    // With ctrl -- should fire
    dispatchKey({ key: 'k', ctrlKey: true });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('matches meta modifier', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'k', meta: true, handler }]),
    );

    // Without meta -- should not fire
    dispatchKey({ key: 'k' });
    expect(handler).not.toHaveBeenCalled();

    // With meta -- should fire
    dispatchKey({ key: 'k', metaKey: true });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('matches ctrl when meta binding specified (ctrlOrMeta logic)', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'k', meta: true, handler }]),
    );

    // ctrl should also match a meta binding
    dispatchKey({ key: 'k', ctrlKey: true });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('matches shift modifier', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'P', shift: true, handler }]),
    );

    // Without shift -- should not fire
    dispatchKey({ key: 'P' });
    expect(handler).not.toHaveBeenCalled();

    // With shift -- should fire
    dispatchKey({ key: 'P', shiftKey: true });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('skips global binding when input is focused', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: '/', global: true, handler }]),
    );

    // Dispatch from an INPUT element so e.target is the input.
    // The event bubbles up to window where the hook listener lives.
    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    input.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }));
    expect(handler).not.toHaveBeenCalled();

    document.body.removeChild(input);
  });

  it('fires global binding when no input is focused', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: '/', global: true, handler }]),
    );

    // Focus on a non-input element
    document.body.focus();
    dispatchKey({ key: '/' });
    expect(handler).toHaveBeenCalledOnce();
  });

  it('skips global binding when textarea is focused', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: '/', global: true, handler }]),
    );

    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    textarea.focus();

    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }));
    expect(handler).not.toHaveBeenCalled();

    document.body.removeChild(textarea);
  });

  it('calls preventDefault on matched binding', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'k', meta: true, handler }]),
    );

    const event = new KeyboardEvent('keydown', {
      key: 'k',
      metaKey: true,
      bubbles: true,
      cancelable: true,
    });
    const spy = vi.spyOn(event, 'preventDefault');
    window.dispatchEvent(event);

    expect(spy).toHaveBeenCalledOnce();
  });

  it('handles multiple bindings, only matching one fires', () => {
    const handlerA = vi.fn();
    const handlerB = vi.fn();
    const handlerC = vi.fn();

    renderHook(() =>
      useKeyboard([
        { key: 'a', handler: handlerA },
        { key: 'b', handler: handlerB },
        { key: 'c', handler: handlerC },
      ]),
    );

    dispatchKey({ key: 'b' });
    expect(handlerA).not.toHaveBeenCalled();
    expect(handlerB).toHaveBeenCalledOnce();
    expect(handlerC).not.toHaveBeenCalled();
  });

  it('does not match when ctrl/meta is pressed but binding has no modifier', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboard([{ key: 'a', handler }]),
    );

    dispatchKey({ key: 'a', ctrlKey: true });
    expect(handler).not.toHaveBeenCalled();

    dispatchKey({ key: 'a', metaKey: true });
    expect(handler).not.toHaveBeenCalled();
  });
});

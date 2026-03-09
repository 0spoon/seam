import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginPage } from './LoginPage';
import { useAuthStore } from '../../stores/authStore';

// Mock useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

beforeEach(() => {
  useAuthStore.setState({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
  });
  mockNavigate.mockClear();
});

function renderLoginPage() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  );
}

describe('LoginPage', () => {
  it('renders the Seam wordmark', () => {
    renderLoginPage();
    expect(screen.getByText('Seam')).toBeInTheDocument();
  });

  it('renders the tagline', () => {
    renderLoginPage();
    expect(screen.getByText('Where ideas connect')).toBeInTheDocument();
  });

  it('renders login form by default', () => {
    renderLoginPage();
    expect(screen.getByLabelText('Username')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByText('Log in')).toBeInTheDocument();
    // Email should NOT be present in login mode
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument();
  });

  it('toggles to register mode', () => {
    renderLoginPage();
    fireEvent.click(screen.getByText('Need an account? Register'));
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByText('Create account')).toBeInTheDocument();
  });

  it('toggles back to login mode', () => {
    renderLoginPage();
    fireEvent.click(screen.getByText('Need an account? Register'));
    fireEvent.click(screen.getByText('Already have an account? Log in'));
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument();
    expect(screen.getByText('Log in')).toBeInTheDocument();
  });

  it('displays error from store', () => {
    useAuthStore.setState({ error: 'Invalid credentials' });
    renderLoginPage();
    expect(screen.getByText('Invalid credentials')).toBeInTheDocument();
  });

  it('fills in username and password fields', () => {
    renderLoginPage();
    const usernameInput = screen.getByLabelText('Username') as HTMLInputElement;
    const passwordInput = screen.getByLabelText('Password') as HTMLInputElement;

    fireEvent.change(usernameInput, { target: { value: 'testuser' } });
    fireEvent.change(passwordInput, { target: { value: 'testpass' } });

    expect(usernameInput.value).toBe('testuser');
    expect(passwordInput.value).toBe('testpass');
  });

  it('disables submit button when loading', () => {
    useAuthStore.setState({ isLoading: true });
    renderLoginPage();
    const button = screen.getByText('Loading...');
    expect(button).toBeDisabled();
  });
});

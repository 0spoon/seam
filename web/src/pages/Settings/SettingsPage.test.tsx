import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SettingsPage } from './SettingsPage';
import { useAuthStore } from '../../stores/authStore';
import { useSettingsStore } from '../../stores/settingsStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { getMe } from '../../api/client';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('../../api/client', () => ({
  getMe: vi.fn(),
  changePassword: vi.fn(),
  updateEmail: vi.fn(),
}));

function renderSettingsPage() {
  return render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({
    user: null,
    isAuthenticated: true,
    isLoading: false,
    error: null,
  });
  useSettingsStore.setState({
    settings: {
      editor_view_mode: 'split',
      right_panel_open: 'true',
      sidebar_collapsed: 'false',
    },
    isLoaded: true,
  });
  useToastStore.setState({ toasts: [] });
  vi.mocked(getMe).mockResolvedValue({
    id: 'test-user-id',
    username: 'testuser',
    email: 'test@example.com',
  });
});

describe('SettingsPage', () => {
  it('renders page title "Settings"', () => {
    renderSettingsPage();
    expect(screen.getByText('Settings')).toBeInTheDocument();
  });

  it('renders Account section', () => {
    renderSettingsPage();
    expect(screen.getByText('Account')).toBeInTheDocument();
  });

  it('renders Appearance section', () => {
    renderSettingsPage();
    expect(screen.getByText('Appearance')).toBeInTheDocument();
  });

  it('renders Keyboard Shortcuts section', () => {
    renderSettingsPage();
    expect(screen.getByText('Keyboard Shortcuts')).toBeInTheDocument();
  });

  it('renders About section with Seam description', () => {
    renderSettingsPage();
    expect(screen.getByText('About')).toBeInTheDocument();
    expect(
      screen.getByText('Seam -- a local-first, AI-powered knowledge system. Where ideas connect.'),
    ).toBeInTheDocument();
  });

  it('displays username from API', async () => {
    renderSettingsPage();
    await waitFor(() => {
      expect(screen.getByText('testuser')).toBeInTheDocument();
    });
  });
});

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '../../stores/authStore';
import { useSettingsStore } from '../../stores/settingsStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { getMe, changePassword, updateEmail } from '../../api/client';
import styles from './SettingsPage.module.css';

const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;
const mod = isMac ? 'Cmd' : 'Ctrl';

const SHORTCUTS = [
  { keys: '/', action: 'Focus sidebar search' },
  { keys: `${mod}+K`, action: 'Open command palette' },
  { keys: `${mod}+N`, action: 'Open quick capture modal' },
  { keys: `${mod}+S`, action: 'Save current note' },
  { keys: `${mod}+\\`, action: 'Toggle sidebar' },
  { keys: `${mod}+B`, action: 'Bold (in editor)' },
  { keys: `${mod}+I`, action: 'Italic (in editor)' },
  { keys: 'Escape', action: 'Close modal / deselect' },
] as const;

export function SettingsPage() {
  const navigate = useNavigate();
  const logout = useAuthStore((s) => s.logout);
  const addToast = useToastStore((s) => s.addToast);

  const settings = useSettingsStore((s) => s.settings);
  const updateSetting = useSettingsStore((s) => s.updateSetting);

  // Account info
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [newEmail, setNewEmail] = useState('');
  const [showEmailForm, setShowEmailForm] = useState(false);
  const [accountLoading, setAccountLoading] = useState(true);

  // Password change
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [passwordLoading, setPasswordLoading] = useState(false);

  useEffect(() => {
    getMe().then((user) => {
      setUsername(user.username);
      setEmail(user.email);
      setNewEmail(user.email);
    }).catch(() => {
      addToast('Failed to load account info', 'error');
    }).finally(() => {
      setAccountLoading(false);
    });
  }, [addToast]);

  const handleChangePassword = async () => {
    if (newPassword !== confirmPassword) {
      addToast('Passwords do not match', 'error');
      return;
    }
    if (newPassword.length < 8) {
      addToast('Password must be at least 8 characters', 'error');
      return;
    }
    setPasswordLoading(true);
    try {
      await changePassword(currentPassword, newPassword);
      addToast('Password changed successfully', 'success');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to change password';
      addToast(message, 'error');
    } finally {
      setPasswordLoading(false);
    }
  };

  const handleUpdateEmail = async () => {
    if (!newEmail.trim() || newEmail === email) {
      setShowEmailForm(false);
      return;
    }
    try {
      await updateEmail(newEmail.trim());
      setEmail(newEmail.trim());
      setShowEmailForm(false);
      addToast('Email updated', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to update email';
      addToast(message, 'error');
    }
  };

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  return (
    <div className={styles.page}>
      <h1 className={styles.pageTitle}>Settings</h1>

      {/* Account */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Account</h2>
        <div className={styles.row}>
          <div>
            <div className={styles.rowLabel}>Username</div>
          </div>
          {accountLoading ? (
            <span className={styles.rowValue}>Loading...</span>
          ) : (
            <span className={styles.rowValue}>{username}</span>
          )}
        </div>
        <div className={styles.row}>
          <div>
            <div className={styles.rowLabel}>Email</div>
          </div>
          {showEmailForm ? (
            <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
              <input
                type="email"
                className={styles.input}
                value={newEmail}
                onChange={(e) => setNewEmail(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleUpdateEmail();
                  if (e.key === 'Escape') { setShowEmailForm(false); setNewEmail(email); }
                }}
                style={{ width: 200 }}
                autoFocus
              />
              <button className={styles.primaryButton} onClick={handleUpdateEmail}>
                Save
              </button>
            </div>
          ) : (
            <button
              className={styles.rowValue}
              onClick={() => setShowEmailForm(true)}
              style={{ cursor: 'pointer' }}
              title="Click to edit"
            >
              {email}
            </button>
          )}
        </div>

        <div style={{ marginTop: 'var(--space-4)' }}>
          <h3 className={styles.formLabel}>Change Password</h3>
          <div className={styles.formGroup}>
            <input
              type="password"
              className={styles.input}
              placeholder="Current password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              autoComplete="current-password"
            />
          </div>
          <div className={styles.formGroup}>
            <input
              type="password"
              className={styles.input}
              placeholder="New password (min 8 chars)"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              autoComplete="new-password"
            />
          </div>
          <div className={styles.formGroup}>
            <input
              type="password"
              className={styles.input}
              placeholder="Confirm new password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleChangePassword(); }}
              autoComplete="new-password"
            />
          </div>
          <div className={styles.buttonRow}>
            <button
              className={styles.primaryButton}
              onClick={handleChangePassword}
              disabled={passwordLoading || !currentPassword || !newPassword || !confirmPassword}
            >
              {passwordLoading ? 'Saving...' : 'Change Password'}
            </button>
          </div>
        </div>

        <div className={styles.buttonRow} style={{ marginTop: 'var(--space-6)' }}>
          <button className={styles.dangerButton} onClick={handleLogout}>
            Log Out
          </button>
        </div>
      </section>

      {/* Appearance */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Appearance</h2>
        <div className={styles.row}>
          <div>
            <div className={styles.rowLabel}>Default editor view</div>
            <div className={styles.rowDescription}>View mode when opening a note</div>
          </div>
          <select
            className={styles.select}
            value={settings.editor_view_mode ?? 'split'}
            onChange={(e) => updateSetting('editor_view_mode', e.target.value)}
          >
            <option value="editor">Editor</option>
            <option value="split">Split</option>
            <option value="preview">Preview</option>
          </select>
        </div>
        <div className={styles.row}>
          <div>
            <div className={styles.rowLabel}>Right panel</div>
            <div className={styles.rowDescription}>Show right panel by default</div>
          </div>
          <select
            className={styles.select}
            value={settings.right_panel_open ?? 'true'}
            onChange={(e) => updateSetting('right_panel_open', e.target.value)}
          >
            <option value="true">Open</option>
            <option value="false">Closed</option>
          </select>
        </div>
        <div className={styles.row}>
          <div>
            <div className={styles.rowLabel}>Sidebar</div>
            <div className={styles.rowDescription}>Default sidebar state</div>
          </div>
          <select
            className={styles.select}
            value={settings.sidebar_collapsed ?? 'false'}
            onChange={(e) => updateSetting('sidebar_collapsed', e.target.value)}
          >
            <option value="false">Expanded</option>
            <option value="true">Collapsed</option>
          </select>
        </div>
      </section>

      {/* Keyboard Shortcuts */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Keyboard Shortcuts</h2>
        <table className={styles.shortcutsTable}>
          <thead>
            <tr>
              <th>Shortcut</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {SHORTCUTS.map((s) => (
              <tr key={s.keys}>
                <td>
                  {s.keys.split('+').map((k, i) => (
                    <span key={k}>
                      {i > 0 && ' + '}
                      <kbd className={styles.kbd}>{k}</kbd>
                    </span>
                  ))}
                </td>
                <td>{s.action}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      {/* About */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>About</h2>
        <p className={styles.aboutText}>
          Seam -- a local-first, AI-powered knowledge system.
        </p>
        <p className={styles.aboutVersion}>
          Version {(typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : '0.0.0')}
        </p>
        <p className={styles.aboutText}>
          <a
            href="https://github.com/katata/seam"
            target="_blank"
            rel="noopener noreferrer"
            className={styles.aboutLink}
          >
            Documentation
          </a>
        </p>
      </section>
    </div>
  );
}

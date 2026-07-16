import type { Dispatch, SetStateAction } from 'react';
import type { WorkspaceInvitationRecord, WorkspaceMemberRecord } from '../../types';
import { inviteStatusMeta, memberStatusMeta } from '../../lib/workspaceLifecycle';
import { ActionButton, Badge, ControlInput, ControlSelect, LabelText } from '../shared';

export function WorkspaceMembershipSection({
  canManageMemberships,
  memberCounts,
  members,
  memberId,
  memberRoles,
  setMemberRoles,
  roleOptionsForMember,
  updatingMemberId,
  removingMemberId,
  onSaveMemberRole,
  onRemoveMember,
  formatDate,
  inviteCounts,
  inviteEmail,
  onInviteEmailChange,
  inviteDisplayName,
  onInviteDisplayNameChange,
  inviteRole,
  onInviteRoleChange,
  visibleInviteRoles,
  creatingInvite,
  onCreateInvite,
  pendingInvites,
  inviteHistory,
  onRevokeInvite,
  onOpenDocs,
}: {
  canManageMemberships: boolean;
  memberCounts: {
    total: number;
    admins: number;
    operators: number;
  };
  members: WorkspaceMemberRecord[];
  memberId?: string;
  memberRoles: Record<string, string>;
  setMemberRoles: Dispatch<SetStateAction<Record<string, string>>>;
  roleOptionsForMember: (currentRole: string) => string[];
  updatingMemberId: string | null;
  removingMemberId: string | null;
  onSaveMemberRole: (memberId: string, currentRole: string) => void;
  onRemoveMember: (memberId: string) => void;
  formatDate: (value?: string | null) => string;
  inviteCounts: {
    pending: number;
    accepted: number;
    expired: number;
    revoked: number;
  };
  inviteEmail: string;
  onInviteEmailChange: (value: string) => void;
  inviteDisplayName: string;
  onInviteDisplayNameChange: (value: string) => void;
  inviteRole: string;
  onInviteRoleChange: (value: string) => void;
  visibleInviteRoles: readonly string[];
  creatingInvite: boolean;
  onCreateInvite: () => void;
  pendingInvites: WorkspaceInvitationRecord[];
  inviteHistory: WorkspaceInvitationRecord[];
  onRevokeInvite: (inviteId: string) => void;
  onOpenDocs: () => void;
}) {
  return (
    <div className="grid-row workspace-members-row" style={{ alignItems: 'start' }}>
      <div className="cell workspace-members-cell" style={{ gridColumn: 'span 2' }}>
        <LabelText as="div" style={{ marginBottom: '1.5rem' }}>MEMBERS</LabelText>
        <div className="workspace-lifecycle-summary">
          <Badge>TOTAL {memberCounts.total}</Badge>
          <Badge>ADMINS {memberCounts.admins}</Badge>
          <Badge>OPERATORS {memberCounts.operators}</Badge>
        </div>
        {canManageMemberships ? (
          members.length > 0 ? (
            <div className="mobile-data-list">
              {members.map((memberRecord) => {
                const isCurrentMember = memberId === memberRecord.id;
                const status = memberStatusMeta(memberRecord, memberId);
                return (
                  <div key={memberRecord.id} className="mobile-data-card">
                    <div className="mobile-data-card-header">
                      <div>
                        <div className="mobile-data-title">{memberRecord.display_name}</div>
                        <div className="mobile-data-subtitle">{memberRecord.email}</div>
                      </div>
                      <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                        <Badge>{(memberRoles[memberRecord.id] || memberRecord.role).toUpperCase()}</Badge>
                        <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                      </div>
                    </div>
                    <div className="mobile-data-meta">
                      <div><LabelText>ACCESS</LabelText> <span>{status.detail}</span></div>
                      <div><LabelText>JOINED</LabelText> <span>{formatDate(memberRecord.created_at)}</span></div>
                    </div>
                    <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
                      <div>
                        <LabelText as="div">ROLE</LabelText>
                        <ControlSelect
                          value={memberRoles[memberRecord.id] || memberRecord.role}
                          disabled={isCurrentMember}
                          onChange={(e) => setMemberRoles((current) => ({ ...current, [memberRecord.id]: e.target.value }))}
                        >
                          {roleOptionsForMember(memberRecord.role).map((candidate) => (
                            <option key={candidate} value={candidate}>{candidate}</option>
                          ))}
                        </ControlSelect>
                      </div>
                      <div className="mobile-data-actions">
                        <ActionButton
                          disabled={updatingMemberId === memberRecord.id || isCurrentMember || (memberRoles[memberRecord.id] || memberRecord.role) === memberRecord.role}
                          onClick={() => onSaveMemberRole(memberRecord.id, memberRecord.role)}
                        >
                          {updatingMemberId === memberRecord.id ? 'SAVING...' : 'SAVE ROLE'}
                        </ActionButton>
                        <ActionButton
                          variant="destructive"
                          disabled={removingMemberId === memberRecord.id || isCurrentMember}
                          onClick={() => onRemoveMember(memberRecord.id)}
                        >
                          {removingMemberId === memberRecord.id ? 'REMOVING...' : 'REMOVE'}
                        </ActionButton>
                      </div>
                      {isCurrentMember && (
                        <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                          You cannot change or remove your own membership from this screen.
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              No members yet.
              <div className="help-actions">
                <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE INVITE</ActionButton>
                <ActionButton onClick={onOpenDocs}>READ TEAM ACCESS DOCS</ActionButton>
              </div>
            </div>
          )
        ) : (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            Membership administration is restricted to workspace owners and admins.
          </div>
        )}
      </div>

      <div className="cell workspace-invites-cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
        <LabelText as="div" style={{ marginBottom: '1.5rem' }}>INVITATIONS</LabelText>
        <div className="workspace-lifecycle-summary">
          <Badge>PENDING {inviteCounts.pending}</Badge>
          <Badge>ACCEPTED {inviteCounts.accepted}</Badge>
          <Badge>EXPIRED {inviteCounts.expired}</Badge>
          <Badge>REVOKED {inviteCounts.revoked}</Badge>
        </div>
        {canManageMemberships ? (
          <>
            <div style={{ display: 'grid', gap: '1rem' }}>
              <div>
                <LabelText as="div">EMAIL</LabelText>
                <ControlInput value={inviteEmail} onChange={(e) => onInviteEmailChange(e.target.value)} placeholder="teammate@example.com" />
              </div>
              <div>
                <LabelText as="div">DISPLAY NAME</LabelText>
                <ControlInput value={inviteDisplayName} onChange={(e) => onInviteDisplayNameChange(e.target.value)} placeholder="Optional" />
              </div>
              <div>
                <LabelText as="div">ROLE</LabelText>
                <ControlSelect value={inviteRole} onChange={(e) => onInviteRoleChange(e.target.value)}>
                  {visibleInviteRoles.map((candidate) => (
                    <option key={candidate} value={candidate}>{candidate}</option>
                  ))}
                </ControlSelect>
              </div>
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.5 }}>
                Entering an email here does not send mail automatically. It creates an invite token for manual sharing.
              </div>
              <ActionButton variant="primary" disabled={creatingInvite} onClick={onCreateInvite}>
                {creatingInvite ? 'CREATING...' : 'CREATE INVITE'}
              </ActionButton>
            </div>

            <div style={{ marginTop: '2rem' }}>
              {pendingInvites.length > 0 ? (
                <div className="mobile-data-list">
                  {pendingInvites.map((invite) => {
                    const status = inviteStatusMeta(invite);
                    return (
                      <div key={invite.id} className="mobile-data-card">
                        <div className="mobile-data-card-header">
                          <div>
                            <div className="mobile-data-title">{invite.display_name || invite.email}</div>
                            <div className="mobile-data-subtitle">{invite.email}</div>
                          </div>
                          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                            <Badge>{invite.role.toUpperCase()}</Badge>
                            <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                          </div>
                        </div>
                        <div className="mobile-data-meta">
                          <div><LabelText>EXPIRES</LabelText> <span>{formatDate(invite.expires_at)}</span></div>
                          <div><LabelText>STATE</LabelText> <span>{status.detail}</span></div>
                        </div>
                        <div className="mobile-data-actions">
                          <ActionButton variant="destructive" onClick={() => onRevokeInvite(invite.id)}>REVOKE</ActionButton>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                  No pending invitations. Accepted, expired, and revoked invites appear in history below.
                  <div className="help-actions">
                    <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE INVITE</ActionButton>
                    <ActionButton onClick={onOpenDocs}>READ INVITE FLOW</ActionButton>
                  </div>
                </div>
              )}
            </div>

            <div style={{ marginTop: '2rem' }}>
              <LabelText as="div" style={{ marginBottom: '1rem' }}>INVITE HISTORY</LabelText>
              {inviteHistory.length > 0 ? (
                <div className="mobile-data-list">
                  {inviteHistory.map((invite) => {
                    const status = inviteStatusMeta(invite);
                    return (
                      <div key={invite.id} className="mobile-data-card">
                        <div className="mobile-data-card-header">
                          <div>
                            <div className="mobile-data-title">{invite.display_name || invite.email}</div>
                            <div className="mobile-data-subtitle">{invite.email}</div>
                          </div>
                          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                            <Badge>{invite.role.toUpperCase()}</Badge>
                            <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                          </div>
                        </div>
                        <div className="mobile-data-meta">
                          <div><LabelText>CREATED</LabelText> <span>{formatDate(invite.created_at)}</span></div>
                          <div><LabelText>FINAL STATE</LabelText> <span>{status.detail}</span></div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                  Invite history will appear once invites are accepted, revoked, or expire.
                  <div className="help-actions">
                    <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE FIRST INVITE</ActionButton>
                    <ActionButton onClick={onOpenDocs}>READ INVITE FLOW</ActionButton>
                  </div>
                </div>
              )}
            </div>
          </>
        ) : (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            Invitation management is restricted to workspace owners and admins.
          </div>
        )}
      </div>
    </div>
  );
}

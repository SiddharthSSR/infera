import { useRef, useState } from 'react';
import { ActionButton, ControlInput, LabelText } from '../shared';
import { SectionHeader } from '../SectionHeader';

export type QuickConfigField = {
  key: string;
  label: string;
  value: string;
  inputType?: 'text' | 'number' | 'select';
  selectOptions?: { value: string; label: string }[];
};

function QuickConfigRow({
  field,
  canEdit,
  onSave,
}: {
  field: QuickConfigField;
  canEdit: boolean;
  onSave: (key: string, value: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(field.value);
  const [saving, setSaving] = useState(false);
  const rowRef = useRef<HTMLDivElement>(null);

  const handleEdit = () => {
    setDraft(field.value);
    setEditing(true);
  };

  const handleCancel = () => {
    setEditing(false);
    setDraft(field.value);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(field.key, draft);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      ref={rowRef}
      className="quick-config-row"
      style={{
        display: 'grid',
        gridTemplateColumns: '2fr 2fr 1fr',
        padding: '0.75rem 0',
        borderBottom: '1px solid #EEEEEC',
        alignItems: 'center',
        transition: 'min-height 0.25s ease',
        minHeight: editing ? 52 : 38,
      }}
    >
      <div style={{ fontSize: '0.9rem' }}>{field.label}</div>
      <div style={{ minHeight: 28 }}>
        {editing ? (
          field.inputType === 'select' && field.selectOptions ? (
            <select
              className="control-input"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              style={{ width: '100%', margin: 0 }}
            >
              {field.selectOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          ) : (
            <ControlInput
              type={field.inputType || 'text'}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              style={{ width: '100%', margin: 0 }}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') void handleSave();
                if (e.key === 'Escape') handleCancel();
              }}
            />
          )
        ) : (
          <span className="mono" style={{ fontSize: '0.85rem' }}>{field.value}</span>
        )}
      </div>
      <div style={{ textAlign: 'right' }}>
        {!canEdit ? null : editing ? (
          <span style={{ display: 'inline-flex', gap: '0.5rem' }}>
            <ActionButton disabled={saving} onClick={() => void handleSave()}>
              {saving ? 'SAVING...' : 'SAVE'}
            </ActionButton>
            <ActionButton onClick={handleCancel}>CANCEL</ActionButton>
          </span>
        ) : (
          <ActionButton onClick={handleEdit}>EDIT</ActionButton>
        )}
      </div>
    </div>
  );
}

export function QuickConfigurationPanel({
  quickConfigFields,
  canEditQuota,
  onSave,
}: {
  quickConfigFields: QuickConfigField[];
  canEditQuota: boolean;
  onSave: (key: string, value: string) => Promise<void>;
}) {
  return (
    <>
      <SectionHeader
        eyebrow="QUICK CONFIGURATION"
        title="Workspace limits"
        description="Edit quota settings inline. Changes are saved to the active workspace immediately."
      />
      <div style={{ marginTop: '1.25rem' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '2fr 2fr 1fr', paddingBottom: '0.5rem', borderBottom: '1px solid var(--text-primary)' }}>
          <LabelText>SETTING</LabelText>
          <LabelText>VALUE</LabelText>
          <LabelText style={{ textAlign: 'right' }}>ACTION</LabelText>
        </div>
        {quickConfigFields.map((field) => (
          <QuickConfigRow
            key={field.key}
            field={field}
            canEdit={canEditQuota}
            onSave={onSave}
          />
        ))}
      </div>
    </>
  );
}

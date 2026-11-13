import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { LogoMark, Wordmark } from './logo';

describe('LogoMark', () => {
  it('renders the tag-with-play-notch path', () => {
    const { container } = render(<LogoMark />);
    const path = container.querySelector('path');
    expect(path).not.toBeNull();
    expect(path?.getAttribute('d')).toContain('M2.5 5 H14.5');
  });
});

describe('Wordmark', () => {
  it('renders the mark alongside the "Carryon" text', () => {
    render(<Wordmark />);
    expect(screen.getByText('Carryon')).toBeTruthy();
  });
});

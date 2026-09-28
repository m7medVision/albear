import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { EnvironmentBadge } from '../renderer/components/EnvironmentBadge';
import { albearMock } from './albearMock';

describe('EnvironmentBadge', () => {
  it('marks the dev environment', () => {
    window.albear = albearMock({ environment: 'dev' });
    render(<EnvironmentBadge />);
    expect(screen.getByText('DEV')).toBeInTheDocument();
  });

  it('shows nothing in prod', () => {
    window.albear = albearMock({ environment: 'prod' });
    const { container } = render(<EnvironmentBadge />);
    expect(container).toBeEmptyDOMElement();
  });
});
